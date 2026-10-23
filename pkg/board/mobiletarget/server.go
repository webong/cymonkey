// Package mobiletarget is the guest side of the mobile.target contract: one
// process per target that owns the WebRTC peer and the bounded media queues,
// accepts the app's frames on a declared loopback endpoint, and serves the
// viewer half through the device provider handler.
package mobiletarget

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"board/rtc"
	"jangolova/devicekit"
	"jangolova/devicekit/serve"
	"jangolova/mobilekit"

	"github.com/gorilla/websocket"
	pion "github.com/pion/webrtc/v4"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/endpoint"
)

const (
	deviceID    = "peer"
	maxICE      = 16
	maxICEURLs  = 8
	maxNameSize = 128
)

// Server serves mobile.target for one process.
type Server struct {
	devices *serve.Handler

	mu      sync.Mutex
	started bool
	target  *target
}

func New() *Server {
	s := &Server{}
	s.devices = serve.NewHandler(s.providers)
	return s
}

func (s *Server) providers(context.Context, map[string]string) ([]devicekit.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.target == nil {
		return nil, errors.New("mobile target is not started")
	}
	return []devicekit.Provider{s.target.provider}, nil
}

// Call dispatches one mobile.target operation. The device operations are the
// device provider handler's, served under this contract's name.
func (s *Server) Call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	operation, ok := strings.CutPrefix(method, mobilekit.Contract+".")
	if !ok {
		return nil, errors.New("unsupported contract")
	}
	switch operation {
	case "start":
		var request mobilekit.StartRequest
		if len(raw) == 0 || json.Unmarshal(raw, &request) != nil {
			return nil, refusal(mobilekit.CodeInvalidTarget)
		}
		return s.start(request)
	case "stop":
		s.stop()
		return struct{}{}, nil
	}
	return s.devices.Call(ctx, "device.provider."+operation, raw)
}

func refusal(code string) error {
	return &plugin.RemoteError{Code: code, Message: strings.ReplaceAll(code, "_", " ")}
}

func (s *Server) start(request mobilekit.StartRequest) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil, errors.New("target is already started")
	}
	if request.ID == "" || len(request.Name) > maxNameSize || !request.Media.Any() || request.TTLSeconds < 1 || request.TTLSeconds > 3600 || len(request.ICEServers) > maxICE {
		return nil, refusal(mobilekit.CodeInvalidTarget)
	}
	servers := make([]pion.ICEServer, 0, len(request.ICEServers))
	for _, server := range request.ICEServers {
		if len(server.URLs) == 0 || len(server.URLs) > maxICEURLs {
			return nil, refusal(mobilekit.CodeInvalidTarget)
		}
		converted := pion.ICEServer{URLs: server.URLs, Username: server.Username}
		if server.Credential != "" {
			converted.Credential = server.Credential
		}
		servers = append(servers, converted)
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, refusal(mobilekit.CodeConverterUnavailable)
	}
	t, err := newTarget(request, servers)
	if err != nil {
		return nil, err
	}
	s.started = true
	s.target = t
	return mobilekit.StartResult{Endpoint: t.endpoint}, nil
}

func (s *Server) stop() {
	s.mu.Lock()
	t := s.target
	s.mu.Unlock()
	if t != nil {
		t.close()
	}
}

// Close ends the target when the host closes the session.
func (s *Server) Close() { s.stop() }

type target struct {
	media    *rtc.MobileMedia
	provider devicekit.Provider
	listener net.Listener
	server   *http.Server
	endpoint endpoint.Endpoint
	granted  mobilekit.Media
	token    []byte
	path     string
	expires  time.Time
	timer    *time.Timer

	mu       sync.Mutex
	attached bool
	conn     *websocket.Conn
	once     sync.Once
}

func newTarget(request mobilekit.StartRequest, servers []pion.ICEServer) (*target, error) {
	ttl := time.Duration(request.TTLSeconds) * time.Second
	media := rtc.NewMobileMedia(request.Media.Video, request.Media.Audio, request.Media.Microphone, request.Media.Playback)
	provider, err := rtc.New(rtc.Options{
		ICEServers: servers, RelayOnly: request.RelayOnly, ID: request.ID, Name: request.Name, TTL: ttl,
		Video: media.Source(mobilekit.KindVideo), Audio: media.Source(mobilekit.KindAudio),
		Microphone: media.Source(mobilekit.KindMicrophone), Playback: media.PlaybackSource(),
	})
	if err != nil {
		media.Close()
		return nil, errors.New("could not create the WebRTC provider")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		media.Close()
		return nil, errors.New("could not listen")
	}
	secret := make([]byte, 48)
	if _, err := rand.Read(secret); err != nil {
		listener.Close()
		media.Close()
		return nil, errors.New("could not create credentials")
	}
	t := &target{
		media: media, provider: provider, listener: listener, granted: request.Media,
		path:    "/" + hex.EncodeToString(secret[:16]),
		token:   []byte(hex.EncodeToString(secret[16:])),
		expires: time.Now().Add(ttl),
	}
	t.endpoint = endpoint.Endpoint{Scheme: "ws", Address: listener.Addr().String(), Path: t.path, Token: string(t.token), ExpiresAt: t.expires}
	mux := http.NewServeMux()
	mux.HandleFunc(t.path, t.handle)
	t.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = t.server.Serve(listener) }()
	t.timer = time.AfterFunc(ttl, t.close)
	return t, nil
}

func (t *target) close() {
	t.once.Do(func() {
		t.timer.Stop()
		t.media.Close()
		t.mu.Lock()
		if t.conn != nil {
			t.conn.Close()
		}
		t.mu.Unlock()
		_ = t.server.Close()
	})
}

// handle accepts the one connection the host relays the app through.
func (t *target) handle(w http.ResponseWriter, r *http.Request) {
	presented := []byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if subtle.ConstantTimeCompare(presented, t.token) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	t.mu.Lock()
	if t.attached {
		t.mu.Unlock()
		http.Error(w, "already attached", http.StatusConflict)
		return
	}
	t.attached = true
	t.mu.Unlock()
	conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	t.mu.Lock()
	t.conn = conn
	t.mu.Unlock()
	defer t.close()
	go t.playback(conn)
	conn.SetReadLimit(mobilekit.MaxFrameBytes)
	_ = conn.SetReadDeadline(t.expires)
	for {
		kind, frame, err := conn.ReadMessage()
		if err != nil || kind != websocket.BinaryMessage {
			return
		}
		if mobilekit.CheckFrame(t.granted, frame) != nil || t.media.Push(frame[0], frame[1:]) != nil {
			return
		}
	}
}

// playback sends the viewer's microphone audio back to the app.
func (t *target) playback(conn *websocket.Conn) {
	for {
		select {
		case data := <-t.media.Playback():
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteMessage(websocket.BinaryMessage, append([]byte{mobilekit.KindPlayback}, data...)); err != nil {
				conn.Close()
				return
			}
		case <-t.media.Done():
			return
		}
	}
}
