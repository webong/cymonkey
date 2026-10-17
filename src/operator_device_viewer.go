package main

import (
	"devicekit"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"cymonkey/src/contracts"
	"cymonkey/src/devicebridge"
)

// Only the operator can issue/rotate this credential. It grants signaling on
// one existing session; the session's Board grant remains the media/input gate.
func (server *operatorServer) issueDeviceViewer(w http.ResponseWriter, r *http.Request, s *managedDeviceSession, id string) {
	if r.Method != http.MethodPost {
		operatorError(w, 405, "method_not_allowed", "POST is required")
		return
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		operatorError(w, 500, "credential_failed", "could not issue viewer credential")
		return
	}
	token := hex.EncodeToString(key[:])
	server.mu.Lock()
	s.viewerHash = sha256.Sum256([]byte(token))
	s.viewerIssued = true
	server.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	operatorJSON(w, 201, map[string]any{"apiVersion": contracts.DeviceAPIVersion, "sessionId": id, "token": token, "path": "/v1/device-viewer/" + id, "expiresAt": s.expires})
}

func (server *operatorServer) handleDeviceViewer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	id := strings.TrimPrefix(r.URL.Path, "/v1/device-viewer/")
	supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	hash := sha256.Sum256([]byte(supplied))
	server.mu.Lock()
	s := server.deviceSessions[id]
	authorized := s != nil && s.viewerIssued && len(supplied) == 64 && subtle.ConstantTimeCompare(hash[:], s.viewerHash[:]) == 1
	server.mu.Unlock()
	if !authorized || time.Now().After(s.expires) {
		operatorError(w, 401, "unauthorized", "valid viewer credential required")
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
	if action.ResourceID != "" || (action.Capability != string(devicekit.WebRTCSignal) && action.Capability != string(devicekit.WebRTCPoll)) {
		operatorError(w, 403, "not_granted", "viewer credential allows session signaling only")
		return
	}
	result, err := s.connection.Invoke(r.Context(), devicebridge.ToLibraryAction(action))
	if err != nil {
		operatorDeviceError(w, err)
		return
	}
	operatorJSON(w, 200, devicebridge.FromLibraryResult(result))
}
