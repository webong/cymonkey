package main

import (
	"context"
	"cymonkey/src/internal/orchestrator"
	"encoding/json"
	"github.com/gorilla/websocket"
	"io"
	"jangolova/devicekit"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOperatorDevicePersistentUploadAndRestore(t *testing.T) {
	state := &storageState{}
	provider := operatorStorageFixture{state: state}
	archive := []byte("transferred")
	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	server.deviceProviders = func() ([]devicekit.Provider, error) { return []devicekit.Provider{provider}, nil }
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
	if err = ws.WriteMessage(websocket.BinaryMessage, archive); err != nil {
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
	if got := state.restoredBytes(); string(got) != "transferred" {
		t.Fatalf("restored %q", got)
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
	server.deviceProviders = func() ([]devicekit.Provider, error) { return []devicekit.Provider{operatorDeviceFixture{}}, nil }
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
	provider := operatorStorageFixture{state: &storageState{}}
	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	server.deviceProviders = func() ([]devicekit.Provider, error) { return []devicekit.Provider{provider}, nil }
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

// operatorStorageFixture is a storage device that accepts an uploaded archive on
// a channel and restores it on request, in memory. It stands in for a real
// storage provider so the operator's session handling is tested on its own.
type operatorStorageFixture struct{ state *storageState }

type storageState struct {
	mu       sync.Mutex
	uploaded []byte
	restored []byte
}

func (s *storageState) restoredBytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.restored...)
}

func (operatorStorageFixture) ID() string { return "storage" }
func (operatorStorageFixture) List(context.Context) ([]devicekit.Device, error) {
	capability := func(name devicekit.Capability) devicekit.CapabilityDescriptor {
		return devicekit.CapabilityDescriptor{Name: name, ResourceRequired: true, InputSchema: json.RawMessage(`{"type":"object"}`)}
	}
	return []devicekit.Device{{ID: "volume", Kind: "storage", Capabilities: []devicekit.CapabilityDescriptor{
		capability("storage.upload"), capability("storage.restore"), capability("storage.export"),
	}}}, nil
}
func (p operatorStorageFixture) Open(context.Context, string, devicekit.Grant) (devicekit.Session, error) {
	return operatorStorageSession{state: p.state}, nil
}

type operatorStorageSession struct{ state *storageState }

func (s operatorStorageSession) Invoke(_ context.Context, action devicekit.Action) (devicekit.Result, error) {
	if action.Capability != "storage.restore" {
		return devicekit.Result{}, devicekit.ErrNotGranted
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.state.restored = append([]byte(nil), s.state.uploaded...)
	return devicekit.Result{Output: json.RawMessage(`{"restored":true}`)}, nil
}
func (s operatorStorageSession) OpenChannel(_ context.Context, action devicekit.Action, _ devicekit.ChannelLimits) (io.ReadWriteCloser, error) {
	if action.Capability != "storage.upload" {
		return nil, devicekit.ErrNotGranted
	}
	return &storageUpload{state: s.state, closed: make(chan struct{})}, nil
}
func (operatorStorageSession) Close(context.Context) error { return nil }

// storageUpload collects what is written and unblocks Read when it is closed.
type storageUpload struct {
	state  *storageState
	closed chan struct{}
	once   sync.Once
}

func (u *storageUpload) Write(data []byte) (int, error) {
	select {
	case <-u.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	u.state.mu.Lock()
	defer u.state.mu.Unlock()
	u.state.uploaded = append(u.state.uploaded, data...)
	return len(data), nil
}
func (u *storageUpload) Read([]byte) (int, error) {
	<-u.closed
	return 0, io.EOF
}
func (u *storageUpload) Close() error {
	u.once.Do(func() { close(u.closed) })
	return nil
}
