package webdriverclassic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"cymonkey/src/internal/bridge"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
)

func TestAdapterUsesExistingSessionAndNeverDeletesIt(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		var value any
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/url"):
			value = "https://example.test/"
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/title"):
			value = "Fixture"
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/window"):
			value = "window-1"
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/element"):
			value = map[string]string{elementKey: "element-1"}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/screenshot"):
			value = "png-data"
		default:
			value = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
	}))
	defer server.Close()

	handle, err := Generic().Connect(context.Background(), manifest.EngineSpec{}, orchestrator.EngineTarget{
		Kind:      "browser",
		Endpoints: []orchestrator.TargetEndpoint{{Name: "webdriver", Protocol: "webdriver", URL: server.URL}},
		Handles:   orchestrator.EngineHandles{"webdriver.sessionId": "safari-session-1"},
	})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	instance := handle.(*instance)

	for method, params := range map[string]json.RawMessage{
		bridge.MethodHello:        json.RawMessage(`{}`),
		bridge.MethodCapabilities: json.RawMessage(`{}`),
		bridge.MethodDescribe:     json.RawMessage(`{}`),
		bridge.MethodAct:          json.RawMessage(`{"name":"window.fill","input":{"selector":"#name","value":"Jango"}}`),
		bridge.MethodEvents:       json.RawMessage(`{"limit":10}`),
	} {
		if result, callErr := instance.Call(context.Background(), method, params); callErr != nil || !json.Valid(result) {
			t.Fatalf("Call(%s) = %s, %v", method, result, callErr)
		}
	}
	if err := instance.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) == 0 {
		t.Fatal("WebDriver received no requests")
	}
	for _, request := range requests {
		if strings.HasPrefix(request, http.MethodDelete+" ") {
			t.Fatalf("adapter attempted to delete caller-owned session: %s", request)
		}
		if !strings.Contains(request, "/session/safari-session-1/") {
			t.Fatalf("request did not use supplied session: %s", request)
		}
	}
}

func TestAdapterRequiresExternalSessionCoordinates(t *testing.T) {
	t.Parallel()
	tests := []orchestrator.EngineTarget{
		{Kind: "native"},
		{Kind: "browser"},
		{Kind: "browser", Endpoints: []orchestrator.TargetEndpoint{{Name: "webdriver", Protocol: "webdriver", URL: "file:///driver"}}},
		{Kind: "browser", Endpoints: []orchestrator.TargetEndpoint{{Name: "webdriver", Protocol: "webdriver", URL: "http://localhost:4444"}}},
	}
	for index, target := range tests {
		if _, err := Generic().Connect(context.Background(), manifest.EngineSpec{}, target); err == nil {
			t.Fatalf("invalid target %d was accepted", index)
		}
	}
}

func TestWebKitAdapterIdentityAndCapability(t *testing.T) {
	t.Parallel()
	inspection := WebKit().InspectEngine(context.Background())
	if !contains(inspection.Capabilities, "target.webkit.webdriver") {
		t.Fatalf("capabilities = %#v", inspection.Capabilities)
	}
	if WebKit().name() != "webkit-webdriver" {
		t.Fatalf("name = %q", WebKit().name())
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
