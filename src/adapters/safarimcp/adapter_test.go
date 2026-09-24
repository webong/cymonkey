package safarimcp

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

func TestAdapterDiscoversAndCallsCallerOwnedSafariMCP(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var methods []string
	var calls []string
	var deletes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()
			deletes++
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		methods = append(methods, request.Method)
		if request.Method == "tools/call" {
			calls = append(calls, request.Params.Name)
			if r.Header.Get("Mcp-Name") != request.Params.Name {
				t.Errorf("Mcp-Name header = %q, want %q", r.Header.Get("Mcp-Name"), request.Params.Name)
			}
		}
		mu.Unlock()
		if request.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "relay-session-1")
		} else if r.Header.Get("Mcp-Session-Id") != "relay-session-1" {
			t.Errorf("session header = %q", r.Header.Get("Mcp-Session-Id"))
		}
		if request.Method != "initialize" && r.Header.Get("MCP-Protocol-Version") != defaultProtocolVersion {
			t.Errorf("protocol header = %q", r.Header.Get("MCP-Protocol-Version"))
		}
		if request.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": defaultProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]string{"name": "safaridriver", "version": "27"},
			}
		case "tools/list":
			result = map[string]any{"tools": []any{
				mcpTool("page_info"),
				mcpTool("navigate_to_url", "url"),
				mcpTool("evaluate_javascript", "script"),
				mcpTool("page_interactions", "actions"),
				mcpTool("screenshot"),
			}}
		case "tools/call":
			result = map[string]any{
				"content":           []any{map[string]string{"type": "text", "text": "ok"}},
				"structuredContent": map[string]any{"tool": request.Params.Name},
			}
		default:
			t.Errorf("unexpected method %q", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID, "result": result,
		})
	}))
	defer server.Close()

	handle, err := (Adapter{}).Connect(context.Background(), manifest.EngineSpec{}, orchestrator.EngineTarget{
		Kind: "browser",
		Endpoints: []orchestrator.TargetEndpoint{{
			Name: "safari-mcp", Protocol: "mcp-streamable-http", URL: server.URL,
		}},
	})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	instance := handle.(*instance)

	requests := map[string]json.RawMessage{
		bridge.MethodHello:        json.RawMessage(`{}`),
		bridge.MethodCapabilities: json.RawMessage(`{}`),
		bridge.MethodDescribe:     json.RawMessage(`{}`),
		bridge.MethodAct:          json.RawMessage(`{"name":"window.navigate","input":{"url":"https://example.test"}}`),
		bridge.MethodEvents:       json.RawMessage(`{"limit":10}`),
	}
	for method, params := range requests {
		result, callErr := instance.Call(context.Background(), method, params)
		if callErr != nil || !json.Valid(result) {
			t.Fatalf("Call(%s) = %s, %v", method, result, callErr)
		}
	}
	for _, action := range []string{
		`{"name":"window.evaluate","input":{"expression":"document.title"}}`,
		`{"name":"mcp.tool.screenshot","input":{}}`,
		`{"name":"mcp.call","input":{"name":"page_interactions","arguments":{"actions":[]}}}`,
	} {
		if _, err := instance.Call(context.Background(), bridge.MethodAct, json.RawMessage(action)); err != nil {
			t.Fatalf("act %s: %v", action, err)
		}
	}
	if health := instance.EngineHealth(context.Background()); health.Status != orchestrator.EngineHealthHealthy {
		t.Fatalf("health = %#v", health)
	}
	if err := instance.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if deletes != 0 {
		t.Fatalf("adapter sent %d DELETE requests", deletes)
	}
	joinedCalls := strings.Join(calls, ",")
	for _, expected := range []string{"page_info", "navigate_to_url", "evaluate_javascript", "screenshot", "page_interactions"} {
		if !strings.Contains(joinedCalls, expected) {
			t.Errorf("tool calls %q do not contain %q", joinedCalls, expected)
		}
	}
	if !contains(methods, "initialize") || !contains(methods, "tools/list") {
		t.Fatalf("methods = %#v", methods)
	}
}

func TestAdapterRejectsNonHTTPOrMissingTarget(t *testing.T) {
	t.Parallel()
	tests := []orchestrator.EngineTarget{
		{Kind: "native"},
		{Kind: "browser"},
		{Kind: "browser", Endpoints: []orchestrator.TargetEndpoint{{Protocol: "mcp-streamable-http", URL: "stdio:///usr/bin/safaridriver"}}},
	}
	for index, target := range tests {
		if _, err := (Adapter{}).Connect(context.Background(), manifest.EngineSpec{}, target); err == nil {
			t.Fatalf("invalid target %d was accepted", index)
		}
	}
}

func TestDecodeSSE(t *testing.T) {
	t.Parallel()
	value, err := decodeSSE([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n"))
	if err != nil || !strings.Contains(string(value.Result), `"ok":true`) {
		t.Fatalf("decodeSSE() = %#v, %v", value, err)
	}
}

func mcpTool(name string, required ...string) map[string]any {
	properties := make(map[string]any, len(required))
	for _, value := range required {
		properties[value] = map[string]string{"type": "string"}
	}
	return map[string]any{
		"name": name,
		"inputSchema": map[string]any{
			"type": "object", "properties": properties, "required": required,
		},
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
