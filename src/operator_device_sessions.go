package main

import (
	"board"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"cymonkey/src/contracts"
	"cymonkey/src/devicebridge"
)

type managedDeviceSession struct {
	connection *board.Connection
	expires    time.Time
	timer      *time.Timer
	once       sync.Once
}

func (s *managedDeviceSession) close() {
	s.once.Do(func() {
		if s.timer != nil {
			s.timer.Stop()
		}
		_ = s.connection.Close(context.Background())
	})
}
func (server *operatorServer) closeDeviceSessions() {
	server.mu.Lock()
	server.deviceClosed = true
	all := server.deviceSessions
	server.deviceSessions = nil
	server.mu.Unlock()
	for _, s := range all {
		s.close()
	}
}
func (server *operatorServer) removeDeviceSession(id string) {
	server.mu.Lock()
	s := server.deviceSessions[id]
	delete(server.deviceSessions, id)
	server.mu.Unlock()
	if s != nil {
		s.close()
	}
}
func (server *operatorServer) handleDeviceSessions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/device-sessions")
	if path == "" {
		if r.Method != http.MethodPost {
			operatorError(w, 405, "method_not_allowed", "POST is required")
			return
		}
		var req struct {
			Open       contracts.DeviceOpenRequest `json:"open"`
			TTLSeconds int                         `json:"ttlSeconds"`
		}
		if err := decodeOperatorJSON(w, r, &req); err != nil {
			operatorError(w, 400, "invalid_request", err.Error())
			return
		}
		if req.TTLSeconds == 0 {
			req.TTLSeconds = 300
		}
		if req.TTLSeconds < 1 || req.TTLSeconds > 3600 {
			operatorError(w, 422, "invalid_lifetime", "ttlSeconds must be between 1 and 3600")
			return
		}
		registry, err := server.deviceRegistry()
		if err != nil {
			operatorDeviceError(w, err)
			return
		}
		conn, err := registry.Open(r.Context(), devicebridge.ToLibraryOpen(req.Open))
		if err != nil {
			operatorDeviceError(w, err)
			return
		}
		var key [24]byte
		if _, err = rand.Read(key[:]); err != nil {
			conn.Close(context.Background())
			operatorDeviceError(w, err)
			return
		}
		id := hex.EncodeToString(key[:])
		s := &managedDeviceSession{connection: conn, expires: time.Now().Add(time.Duration(req.TTLSeconds) * time.Second)}
		server.mu.Lock()
		if server.deviceClosed || len(server.deviceSessions) >= 64 {
			server.mu.Unlock()
			s.close()
			operatorError(w, 503, "session_limit", "device session capacity unavailable")
			return
		}
		if server.deviceSessions == nil {
			server.deviceSessions = map[string]*managedDeviceSession{}
		}
		server.deviceSessions[id] = s
		s.timer = time.AfterFunc(time.Until(s.expires), func() { server.removeDeviceSession(id) })
		server.mu.Unlock()
		operatorJSON(w, 201, map[string]any{"sessionId": id, "expiresAt": s.expires})
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	server.mu.Lock()
	s := server.deviceSessions[parts[0]]
	server.mu.Unlock()
	if s == nil || time.Now().After(s.expires) {
		operatorError(w, 404, "unknown_session", "device session not found or expired")
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodDelete {
			operatorError(w, 405, "method_not_allowed", "DELETE is required")
			return
		}
		server.removeDeviceSession(parts[0])
		w.WriteHeader(204)
		return
	}
	if parts[1] == "events" {
		if r.Method != http.MethodGet {
			operatorError(w, 405, "method_not_allowed", "GET is required")
			return
		}
		var after uint64
		if value := r.URL.Query().Get("after"); value != "" {
			var err error
			after, err = strconv.ParseUint(value, 10, 64)
			if err != nil {
				operatorError(w, 400, "invalid_cursor", "after must be an unsigned event cursor")
				return
			}
		}
		batch, err := s.connection.Events(after)
		if err != nil {
			operatorJSON(w, 409, map[string]any{"code": "cursor_expired", "message": err.Error(), "cursor": batch.Cursor})
			return
		}
		operatorJSON(w, 200, devicebridge.FromLibraryEvents(batch))
		return
	}
	if parts[1] == "channels" {
		server.deviceChannel(w, r, s)
		return
	}
	if parts[1] != "actions" && parts[1] != "streams" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		operatorError(w, 405, "method_not_allowed", "POST is required")
		return
	}
	var action contracts.DeviceAction
	if err := decodeOperatorJSON(w, r, &action); err != nil {
		operatorError(w, 400, "invalid_request", err.Error())
		return
	}
	if parts[1] == "actions" {
		v, err := s.connection.Invoke(r.Context(), devicebridge.ToLibraryAction(action))
		if err != nil {
			operatorDeviceError(w, err)
			return
		}
		operatorJSON(w, 200, devicebridge.FromLibraryResult(v))
		return
	}
	content, err := s.connection.Stream(r.Context(), devicebridge.ToLibraryAction(action))
	if err != nil {
		operatorDeviceError(w, err)
		return
	}
	defer content.Close()
	if content.Size() < 0 || content.Size() > 1<<30 {
		operatorError(w, 413, "stream_limit", "session streams are limited to 1 GiB")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(content.Size(), 10))
	if content.Truncated() {
		w.Header().Set("X-Cymonkey-Truncated", "true")
	}
	_, _ = io.CopyN(w, content, content.Size())
}

// Channels use one JSON setup message followed by binary messages. Empty text
// "close" is not data: a {"close":true} control message commits a write channel
// close, acknowledged with {"closed":true} before the WebSocket closes.
func (server *operatorServer) deviceChannel(w http.ResponseWriter, r *http.Request, s *managedDeviceSession) {
	if r.Method != http.MethodGet {
		operatorError(w, 405, "method_not_allowed", "WebSocket GET is required")
		return
	}
	upgrader := websocket.Upgrader{ReadBufferSize: 32768, WriteBufferSize: 32768}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(64 << 10)
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	var req struct {
		Action          contracts.DeviceAction `json:"action"`
		ReadBytes       int64                  `json:"readBytes"`
		WriteBytes      int64                  `json:"writeBytes"`
		DurationSeconds int                    `json:"durationSeconds"`
	}
	kind, data, err := ws.ReadMessage()
	if err != nil || kind != websocket.TextMessage {
		return
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&req); err != nil {
		return
	}
	var tail any
	if dec.Decode(&tail) != io.EOF {
		return
	}
	if req.DurationSeconds < 1 || req.DurationSeconds > 3600 {
		return
	}
	limits := board.ChannelLimits{ReadBytes: req.ReadBytes, WriteBytes: req.WriteBytes, Duration: time.Duration(req.DurationSeconds) * time.Second}
	ctx, cancel := context.WithTimeout(r.Context(), limits.Duration)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stop()
	ch, err := s.connection.OpenChannel(ctx, devicebridge.ToLibraryAction(req.Action), limits)
	if err != nil {
		_ = ws.WriteJSON(map[string]string{"error": err.Error()})
		return
	}
	defer ch.Close()
	_ = ws.SetReadDeadline(s.expires)
	var writeMu sync.Mutex
	send := func(kind int, b []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
		return ws.WriteMessage(kind, b)
	}
	if err := send(websocket.TextMessage, []byte(`{"opened":true}`)); err != nil {
		return
	}
	done := make(chan struct{})
	if limits.ReadBytes > 0 {
		go func() {
			defer close(done)
			buf := make([]byte, 32768)
			for {
				n, err := ch.Read(buf)
				if n > 0 {
					if e := send(websocket.BinaryMessage, buf[:n]); e != nil {
						break
					}
				}
				if err != nil {
					if !errors.Is(err, io.EOF) {
						_ = send(websocket.TextMessage, []byte(`{"ended":true}`))
					}
					break
				}
			}
			cancel()
		}()
	} else {
		close(done)
	}
	defer func() { cancel(); _ = ch.Close(); <-done }()
	for {
		kind, data, err = ws.ReadMessage()
		if err != nil {
			return
		}
		if kind == websocket.TextMessage {
			var control struct {
				Close bool `json:"close"`
			}
			if json.Unmarshal(data, &control) == nil && control.Close {
				err = ch.Close()
				if err == nil {
					_ = send(websocket.TextMessage, []byte(`{"closed":true}`))
				}
				return
			}
			return
		}
		if kind != websocket.BinaryMessage || limits.WriteBytes == 0 {
			return
		}
		if _, err = ch.Write(data); err != nil {
			return
		}
	}
}
