package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"jangolova/devicekit"
	"jangolova/mobilekit"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
)

const (
	maxMobileTargets = 16
	// mobileStartTimeout bounds launching the package and starting the target.
	mobileStartTimeout = 20 * time.Second
	// maxMobilePlayback bounds a playback frame from the target: one kind byte
	// and at most 160 G.711 samples expanded to 48 kHz stereo PCM.
	maxMobilePlayback = 1 + 160*24
)

// mobileBackend is a started mobile target: the package's process as the host
// uses it. *mobilekit.Target is the real one.
type mobileBackend interface {
	Provider() devicekit.Provider
	Dial(context.Context) (*websocket.Conn, error)
	Done() <-chan struct{}
	Close() error
}

type mobileTarget struct {
	backend  mobileBackend
	provider devicekit.Provider
	media    mobilekit.Media
	hash     [32]byte
	expires  time.Time
	timer    *time.Timer
	once     sync.Once
	attached bool
}

func (t *mobileTarget) close() {
	t.once.Do(func() {
		if t.timer != nil {
			t.timer.Stop()
		}
		_ = t.backend.Close()
	})
}

func (s *operatorServer) removeMobileTarget(id string) {
	s.mu.Lock()
	t := s.mobileTargets[id]
	delete(s.mobileTargets, id)
	s.mu.Unlock()
	if t != nil {
		t.close()
	}
}

// startMobileTarget launches the mobile-target package for one target.
func (s *operatorServer) startMobileTarget(request mobilekit.StartRequest) (mobileBackend, error) {
	if s.mobileStart != nil {
		return s.mobileStart(request)
	}
	installed, err := mobileTargetPackage()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), mobileStartTimeout)
	defer cancel()
	return mobilekit.Start(ctx, installed, request)
}

var errNoMobileTargetPackage = errors.New("no mobile-target package is installed")

func mobileTargetPackage() (store.Installed, error) {
	installed, err := installedPlugins()
	if err != nil {
		return store.Installed{}, err
	}
	for _, item := range installed {
		if item.Supports(mobilekit.Contract, mobilekit.Version) {
			return item, nil
		}
	}
	return store.Installed{}, errNoMobileTargetPackage
}

func (s *operatorServer) createMobileTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		operatorError(w, 405, "method_not_allowed", "POST is required")
		return
	}
	var req struct {
		ICEServers []mobilekit.ICEServer `json:"iceServers,omitempty"`
		RelayOnly  bool                  `json:"relayOnly,omitempty"`
		Name       string                `json:"name"`
		Video      bool                  `json:"video"`
		Audio      bool                  `json:"audio"`
		Microphone bool                  `json:"microphone"`
		Playback   bool                  `json:"playback"`
		TTLSeconds int                   `json:"ttlSeconds"`
	}
	if err := decodeOperatorJSON(w, r, &req); err != nil {
		operatorError(w, 400, "invalid_request", err.Error())
		return
	}
	if req.TTLSeconds == 0 {
		req.TTLSeconds = 300
	}
	media := mobilekit.Media{Video: req.Video, Audio: req.Audio, Microphone: req.Microphone, Playback: req.Playback}
	if req.TTLSeconds < 1 || req.TTLSeconds > 3600 || len(req.Name) > 128 || !media.Any() {
		operatorError(w, 422, "invalid_target", "bounded lifetime and at least one media grant required")
		return
	}
	if !s.mobileCapacity() {
		operatorError(w, 503, "target_limit", "mobile target capacity unavailable")
		return
	}
	var key [32]byte
	var identity [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		operatorError(w, 500, "credential_failed", "could not create target")
		return
	}
	if _, err := rand.Read(identity[:]); err != nil {
		operatorError(w, 500, "credential_failed", "could not create target")
		return
	}
	token := hex.EncodeToString(key[:])
	id := "mobile-" + hex.EncodeToString(identity[:])
	backend, err := s.startMobileTarget(mobilekit.StartRequest{ID: id, Name: req.Name, ICEServers: req.ICEServers, RelayOnly: req.RelayOnly, Media: media, TTLSeconds: req.TTLSeconds})
	switch {
	case errors.Is(err, errNoMobileTargetPackage):
		operatorError(w, 503, "mobile_target_unavailable", "no mobile-target package is installed; build ./pkg/board/cmd/mobile and install it with cymy plugins")
		return
	case errors.Is(err, mobilekit.ErrConverterUnavailable):
		operatorError(w, 503, "converter_unavailable", "ffmpeg is required")
		return
	case errors.Is(err, mobilekit.ErrInvalidTarget):
		operatorError(w, 422, "invalid_target", "the mobile-target package refused the target options")
		return
	case err != nil:
		// The package's own text is untrusted and may name configuration.
		operatorError(w, 502, "mobile_target_failed", "the mobile-target package could not start the target")
		return
	}
	t := &mobileTarget{backend: backend, provider: backend.Provider(), media: media, hash: sha256.Sum256([]byte(token)), expires: time.Now().Add(time.Duration(req.TTLSeconds) * time.Second)}
	s.mu.Lock()
	if s.deviceClosed || len(s.mobileTargets) >= maxMobileTargets {
		s.mu.Unlock()
		t.close()
		operatorError(w, 503, "target_limit", "mobile target capacity unavailable")
		return
	}
	if s.mobileTargets == nil {
		s.mobileTargets = map[string]*mobileTarget{}
	}
	s.mobileTargets[id] = t
	t.timer = time.AfterFunc(time.Until(t.expires), func() { s.removeMobileTarget(id) })
	s.mu.Unlock()
	// A package that dies takes its target with it.
	go func() {
		<-backend.Done()
		s.removeMobileTarget(id)
	}()
	w.Header().Set("Cache-Control", "no-store")
	operatorJSON(w, 201, map[string]any{"apiVersion": "cymonkey.device/v1alpha1", "adapterId": id, "deviceId": "peer", "path": "/v1/device-targets/" + id, "token": token, "expiresAt": t.expires, "media": map[string]bool{"video": req.Video, "audio": req.Audio, "microphone": req.Microphone, "playback": req.Playback}})
}

// mobileCapacity reports whether another target may start.
func (s *operatorServer) mobileCapacity() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.deviceClosed && len(s.mobileTargets) < maxMobileTargets
}

func (s *operatorServer) handleMobileTarget(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/device-targets/")
	if r.Method == http.MethodDelete && s.authorized(r) {
		s.removeMobileTarget(id)
		w.WriteHeader(204)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	hash := sha256.Sum256([]byte(token))
	s.mu.Lock()
	t := s.mobileTargets[id]
	valid := t != nil && len(token) == 64 && subtle.ConstantTimeCompare(hash[:], t.hash[:]) == 1
	if valid {
		valid = !t.attached && time.Now().Before(t.expires)
		if valid {
			t.attached = true
		}
	}
	s.mu.Unlock()
	if !valid {
		operatorError(w, 401, "unauthorized", "valid unattached target credential required")
		return
	}
	defer s.removeMobileTarget(id)
	if r.Method != http.MethodGet {
		operatorError(w, 405, "method_not_allowed", "WebSocket GET is required")
		return
	}
	guest, err := t.backend.Dial(r.Context())
	if err != nil {
		operatorError(w, 502, "mobile_target_failed", "the mobile target is not reachable")
		return
	}
	defer guest.Close()
	ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	stop := time.AfterFunc(time.Until(t.expires), func() { ws.Close() })
	defer stop.Stop()
	go func() { <-t.backend.Done(); ws.Close() }()
	relayPlayback(ws, guest, t.media.Playback)
	ws.SetReadLimit(mobilekit.MaxFrameBytes)
	_ = ws.SetReadDeadline(t.expires)
	var total int64
	for {
		kind, frame, err := ws.ReadMessage()
		if err != nil || kind != websocket.BinaryMessage {
			return
		}
		total += int64(len(frame))
		if total > mobilekit.MaxTotalBytes || mobilekit.CheckFrame(t.media, frame) != nil {
			return
		}
		_ = guest.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := guest.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return
		}
	}
}

// relayPlayback forwards the target's playback frames to the app. Only
// playback frames, of the size the target produces, pass; anything else ends
// the attachment.
func relayPlayback(app, guest *websocket.Conn, allowed bool) {
	guest.SetReadLimit(maxMobilePlayback)
	go func() {
		defer app.Close()
		for {
			kind, frame, err := guest.ReadMessage()
			if err != nil || kind != websocket.BinaryMessage || !allowed || len(frame) < 2 || frame[0] != mobilekit.KindPlayback {
				return
			}
			_ = app.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := app.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				return
			}
		}
	}()
}
