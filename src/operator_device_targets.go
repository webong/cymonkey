package main

import (
	"devicekit"
	"board/rtc"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"image/jpeg"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	pion "github.com/pion/webrtc/v4"
)

type mobileTarget struct {
	media    *rtc.MobileMedia
	provider devicekit.Provider
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
		t.media.Close()
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
func (s *operatorServer) createMobileTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		operatorError(w, 405, "method_not_allowed", "POST is required")
		return
	}
	var req struct {
		ICEServers []pion.ICEServer `json:"iceServers,omitempty"`
		RelayOnly  bool             `json:"relayOnly,omitempty"`
		Name       string           `json:"name"`
		Video      bool             `json:"video"`
		Audio      bool             `json:"audio"`
		Microphone bool             `json:"microphone"`
		Playback   bool             `json:"playback"`
		TTLSeconds int              `json:"ttlSeconds"`
	}
	if err := decodeOperatorJSON(w, r, &req); err != nil {
		operatorError(w, 400, "invalid_request", err.Error())
		return
	}
	if req.TTLSeconds == 0 {
		req.TTLSeconds = 300
	}
	if req.TTLSeconds < 1 || req.TTLSeconds > 3600 || len(req.Name) > 128 || !req.Video && !req.Audio && !req.Microphone && !req.Playback {
		operatorError(w, 422, "invalid_target", "bounded lifetime and at least one media grant required")
		return
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		operatorError(w, 503, "converter_unavailable", "ffmpeg is required")
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
	m := rtc.NewMobileMedia(req.Video, req.Audio, req.Microphone, req.Playback)
	p, err := rtc.New(rtc.Options{ICEServers: req.ICEServers, RelayOnly: req.RelayOnly, ID: id, Name: req.Name, TTL: time.Duration(req.TTLSeconds) * time.Second, Video: m.Source(1), Audio: m.Source(2), Microphone: m.Source(3), Playback: m.PlaybackSource()})
	if err != nil {
		m.Close()
		operatorDeviceError(w, err)
		return
	}
	t := &mobileTarget{media: m, provider: p, hash: sha256.Sum256([]byte(token)), expires: time.Now().Add(time.Duration(req.TTLSeconds) * time.Second)}
	s.mu.Lock()
	if s.deviceClosed || len(s.mobileTargets) >= 16 {
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
	w.Header().Set("Cache-Control", "no-store")
	operatorJSON(w, 201, map[string]any{"apiVersion": "cymonkey.device/v1alpha1", "adapterId": id, "deviceId": "peer", "path": "/v1/device-targets/" + id, "token": token, "expiresAt": t.expires, "media": map[string]bool{"video": req.Video, "audio": req.Audio, "microphone": req.Microphone, "playback": req.Playback}})
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
	ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	stop := time.AfterFunc(time.Until(t.expires), func() { ws.Close() })
	defer stop.Stop()
	go func() { <-t.media.Done(); ws.Close() }()
	ws.SetReadLimit((4 << 20) + 1)
	_ = ws.SetReadDeadline(t.expires)
	go func() {
		for {
			select {
			case data := <-t.media.Playback():
				_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{4}, data...)); err != nil {
					ws.Close()
					return
				}
			case <-t.media.Done():
				return
			}
		}
	}()
	var total int64
	for {
		kind, frame, err := ws.ReadMessage()
		if err != nil {
			return
		}
		total += int64(len(frame))
		if total > 1<<30 || kind != websocket.BinaryMessage || len(frame) < 2 {
			return
		}
		if frame[0] == 1 {
			image, err := jpeg.DecodeConfig(bytes.NewReader(frame[1:]))
			if err != nil || image.Width <= 0 || image.Height <= 0 || image.Width > 4096 || image.Height > 4096 || image.Width*image.Height > 8<<20 {
				return
			}
		}
		if err := t.media.Push(frame[0], frame[1:]); err != nil {
			return
		}
	}
}
