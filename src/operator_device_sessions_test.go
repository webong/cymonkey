package main

import (
	"board"
	"board/devices"
	"board/volumearchive"
	"bytes"
	"context"
	"cymonkey/src/internal/orchestrator"
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOperatorDevicePersistentUploadAndRestore(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("transferred"), 0600); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := volumearchive.Export(context.Background(), &archive, source); err != nil {
		t.Fatal(err)
	}
	provider, err := devices.Storage(devices.StorageOptions{ID: "storage", Path: destination, RootID: "root", AllowRestore: true})
	if err != nil {
		t.Fatal(err)
	}
	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	server.deviceProviders = func() ([]board.Provider, error) { return []board.Provider{provider}, nil }
	opened := operatorTestRequest(server, "secret", http.MethodPost, "/v1/device-sessions", `{"open":{"adapterId":"storage","deviceId":"volume","grant":{"capabilities":["storage.upload","storage.restore"],"resourceIds":["root"]}},"ttlSeconds":60}`)
	if opened.Code != 201 {
		t.Fatalf("open %d %s", opened.Code, opened.Body.String())
	}
	var payload struct {
		SessionID string `json:"sessionId"`
	}
	if err = json.Unmarshal(opened.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	path := "/v1/device-sessions/" + payload.SessionID
	denied := operatorTestRequest(server, "secret", http.MethodPost, path+"/actions", `{"capability":"storage.export","resourceId":"root"}`)
	if denied.Code != 403 {
		t.Fatalf("grant was widened: %d", denied.Code)
	}
	httpServer := httptest.NewServer(server.Routes())
	defer httpServer.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+path+"/channels", http.Header{"Authorization": []string{"Bearer secret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err = ws.WriteJSON(map[string]any{"action": map[string]string{"capability": "storage.upload", "resourceId": "root"}, "writeBytes": 1 << 20, "durationSeconds": 10}); err != nil {
		t.Fatal(err)
	}
	var ack map[string]bool
	if err = ws.ReadJSON(&ack); err != nil || !ack["opened"] {
		t.Fatalf("open ack %+v %v", ack, err)
	}
	if err = ws.WriteMessage(websocket.BinaryMessage, archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = ws.WriteJSON(map[string]bool{"close": true}); err != nil {
		t.Fatal(err)
	}
	ack = nil
	if err = ws.ReadJSON(&ack); err != nil || !ack["closed"] {
		t.Fatalf("close ack %+v %v", ack, err)
	}
	restored := operatorTestRequest(server, "secret", http.MethodPost, path+"/actions", `{"capability":"storage.restore","resourceId":"root"}`)
	if restored.Code != 200 {
		t.Fatalf("restore %d %s", restored.Code, restored.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(destination, "file"))
	if err != nil || string(data) != "transferred" {
		t.Fatalf("file %q %v", data, err)
	}
	events := operatorTestRequest(server, "secret", http.MethodGet, path+"/events", "")
	if events.Code != 200 || !strings.Contains(events.Body.String(), "stream.closed") {
		t.Fatalf("events %d %s", events.Code, events.Body.String())
	}
	closed := operatorTestRequest(server, "secret", http.MethodDelete, path, "")
	if closed.Code != 204 {
		t.Fatal(closed.Code)
	}
	missing := operatorTestRequest(server, "secret", http.MethodPost, path+"/actions", `{"capability":"storage.restore","resourceId":"root"}`)
	if missing.Code != 404 {
		t.Fatal(missing.Code)
	}
}
func TestOperatorDeviceSessionExpiry(t *testing.T) {
	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	server.deviceProviders = func() ([]board.Provider, error) { return []board.Provider{operatorDeviceFixture{}}, nil }
	opened := operatorTestRequest(server, "secret", http.MethodPost, "/v1/device-sessions", `{"open":{"adapterId":"fixture","deviceId":"drive","grant":{"capabilities":["drive.read"],"resourceIds":["photos"]}},"ttlSeconds":1}`)
	if opened.Code != 201 {
		t.Fatal(opened.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		server.mu.Lock()
		n := len(server.deviceSessions)
		server.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session did not expire")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOperatorDeviceRevocationClosesWriteOnlyChannel(t *testing.T) {
	provider, err := devices.Storage(devices.StorageOptions{ID: "storage", Path: t.TempDir(), RootID: "root", AllowRestore: true})
	if err != nil {
		t.Fatal(err)
	}
	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	server.deviceProviders = func() ([]board.Provider, error) { return []board.Provider{provider}, nil }
	response := operatorTestRequest(server, "secret", http.MethodPost, "/v1/device-sessions", `{"open":{"adapterId":"storage","deviceId":"volume","grant":{"capabilities":["storage.upload"],"resourceIds":["root"]}},"ttlSeconds":300}`)
	if response.Code != 201 {
		t.Fatalf("open: %d %s", response.Code, response.Body.String())
	}
	var opened struct {
		APIVersion string `json:"apiVersion"`
		SessionID  string `json:"sessionId"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &opened); err != nil {
		t.Fatal(err)
	}
	if opened.APIVersion != "cymonkey.device/v1alpha1" {
		t.Fatalf("missing session version: %+v", opened)
	}
	path := "/v1/device-sessions/" + opened.SessionID
	listener := httptest.NewServer(server.Routes())
	defer listener.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(listener.URL, "http")+path+"/channels", http.Header{"Authorization": []string{"Bearer secret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err = ws.WriteJSON(map[string]any{"action": map[string]string{"capability": "storage.upload", "resourceId": "root"}, "writeBytes": 1024, "durationSeconds": 300}); err != nil {
		t.Fatal(err)
	}
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var ack map[string]bool
	if err = ws.ReadJSON(&ack); err != nil || !ack["opened"] {
		t.Fatalf("open ack: %+v %v", ack, err)
	}
	closed := operatorTestRequest(server, "secret", http.MethodDelete, path, "")
	if closed.Code != 204 {
		t.Fatal(closed.Body.String())
	}
	_, _, err = ws.ReadMessage()
	if err == nil {
		t.Fatal("revoked channel remained open")
	}
	if strings.Contains(err.Error(), "timeout") {
		t.Fatalf("channel waited for deadline rather than revocation: %v", err)
	}
}
