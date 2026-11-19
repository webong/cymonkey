// Package vncgateway exposes a private VNC, RDP, or SPICE stream through an
// authenticated binary WebSocket transport. It proxies bytes only:
// protocol-aware rendering stays in the selected viewer client.
package vncgateway

import (
	"bufio"
	"context"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type Server struct {
	Token        string
	Upstream     string
	DialTimeout  time.Duration
	MaxFrameSize uint64
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	for _, transport := range []string{"vnc", "rdp", "spice"} {
		mux.HandleFunc("/v1/"+transport, s.handleTransport(transport))
	}
	return mux
}

func (s Server) handleTransport(transport string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.handleStream(w, r, transport)
	}
}

func (s Server) handleStream(w http.ResponseWriter, r *http.Request, transport string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !secureTokenEqual(r.Header.Get("Authorization"), "Bearer "+s.Token) {
		http.Error(w, "authorization required", http.StatusUnauthorized)
		return
	}
	if !headerContains(r.Header, "Connection", "upgrade") ||
		!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		r.Header.Get("Sec-WebSocket-Version") != "13" ||
		strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key")) == "" {
		http.Error(w, "WebSocket upgrade required", http.StatusUpgradeRequired)
		return
	}
	upstream := strings.TrimSpace(s.Upstream)
	if upstream == "" {
		http.Error(w, transport+" upstream is unavailable", http.StatusServiceUnavailable)
		return
	}
	timeout := s.DialTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	vnc, err := net.DialTimeout("tcp", upstream, timeout)
	if err != nil {
		http.Error(w, "connect "+transport+" upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = vnc.Close()
		http.Error(w, "WebSocket hijacking is unavailable", http.StatusInternalServerError)
		return
	}
	client, rw, err := hijacker.Hijack()
	if err != nil {
		_ = vnc.Close()
		return
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	_, _ = fmt.Fprintf(
		rw,
		"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n",
		websocketAccept(key),
	)
	if err := rw.Flush(); err != nil {
		_ = client.Close()
		_ = vnc.Close()
		return
	}
	s.proxy(r.Context(), client, rw.Reader, vnc)
}

func (s Server) proxy(ctx context.Context, client net.Conn, reader *bufio.Reader, vnc net.Conn) {
	defer client.Close()
	defer vnc.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	writes := &sync.Mutex{}
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		_ = proxyWebSocketToVNC(ctx, reader, client, writes, vnc, s.maxFrameSize())
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		_ = proxyVNCToWebSocket(ctx, vnc, client, writes)
	}()
	<-done
	cancel()
}

func (s Server) maxFrameSize() uint64 {
	if s.MaxFrameSize == 0 {
		return 16 * 1024 * 1024
	}
	return s.MaxFrameSize
}

func proxyWebSocketToVNC(ctx context.Context, reader *bufio.Reader, client net.Conn, writes *sync.Mutex, vnc io.Writer, maxFrameSize uint64) error {
	for {
		opcode, payload, err := readClientFrame(reader, maxFrameSize)
		if err != nil {
			return err
		}
		switch opcode {
		case 0x0, 0x2:
			if _, err := vnc.Write(payload); err != nil {
				return err
			}
		case 0x8:
			return io.EOF
		case 0x9:
			writes.Lock()
			err := writeServerFrame(client, 0xA, payload)
			writes.Unlock()
			if err != nil {
				return err
			}
		case 0xA:
		default:
			return fmt.Errorf("unsupported WebSocket opcode %d", opcode)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func proxyVNCToWebSocket(ctx context.Context, vnc io.Reader, client io.Writer, writes *sync.Mutex) error {
	buffer := make([]byte, 32*1024)
	for {
		count, err := vnc.Read(buffer)
		if count > 0 {
			writes.Lock()
			writeErr := writeServerFrame(client, 0x2, buffer[:count])
			writes.Unlock()
			if writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func readClientFrame(reader *bufio.Reader, maxFrameSize uint64) (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, nil, err
	}
	opcode := header[0] & 0x0f
	if header[1]&0x80 == 0 {
		return 0, nil, errors.New("client WebSocket frame is not masked")
	}
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var value uint16
		if err := binary.Read(reader, binary.BigEndian, &value); err != nil {
			return 0, nil, err
		}
		length = uint64(value)
	case 127:
		if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
			return 0, nil, err
		}
	}
	if length > maxFrameSize {
		return 0, nil, errors.New("WebSocket frame exceeds configured limit")
	}
	mask := make([]byte, 4)
	if _, err := io.ReadFull(reader, mask); err != nil {
		return 0, nil, err
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	for index := range payload {
		payload[index] ^= mask[index%4]
	}
	return opcode, payload, nil
}

func writeServerFrame(writer io.Writer, opcode byte, payload []byte) error {
	header := []byte{0x80 | opcode}
	switch length := len(payload); {
	case length < 126:
		header = append(header, byte(length))
	case length <= 65535:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		header = append(header, 127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(header[2:], uint64(length))
	}
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err := writer.Write(payload)
	return err
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + websocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func headerContains(header http.Header, name, value string) bool {
	for _, entry := range header.Values(name) {
		for _, part := range strings.Split(entry, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return true
			}
		}
	}
	return false
}

func secureTokenEqual(got, want string) bool {
	if got == "" || want == "Bearer " {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
