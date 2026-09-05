package cymonkey_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"cymonkey/internal/bridge"
	"cymonkey/internal/manifest"
	"cymonkey/internal/orchestrator"
	cymonkey "cymonkey/lib/jangolova"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func TestCymonkeyEngineBackendNativeProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var msg struct {
				ID     uint64          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			switch msg.Method {
			case "hello":
				_ = conn.WriteJSON(map[string]any{
					"id": msg.ID,
					"result": map[string]any{
						"protocolVersion": "cymonkey/v1alpha1",
						"implementation":  map[string]any{"name": "godot-native", "version": "0.2.0"},
						"domains":         []string{"render"},
						"runtimes":        []string{"godot"},
						"drivers":         []string{"websocket"},
					},
				})
			case "capabilities":
				_ = conn.WriteJSON(map[string]any{
					"id": msg.ID,
					"result": []map[string]any{{
						"name":          "object.visible.set",
						"domain":        "render",
						"runtime":       "godot",
						"driver":        "websocket",
						"support":       "native",
						"lifetime":      "attachment",
						"persistence":   "session",
						"effect":        "write",
						"resourceKinds": []string{"object"},
						"inputSchema":   json.RawMessage(`{}`),
					}},
				})
			case "describe":
				_ = conn.WriteJSON(map[string]any{
					"id": msg.ID,
					"result": map[string]any{
						"revision": "rev-7",
						"surfaces": []map[string]any{{
							"id": "object:door", "domain": "render", "runtime": "godot", "kind": "object", "label": "Front door",
						}},
						"augmentations": []map[string]any{},
					},
				})
			case "act":
				var payload struct {
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				}
				if err := json.Unmarshal(msg.Params, &payload); err != nil || payload.Name == "" {
					_ = conn.WriteJSON(map[string]any{"id": msg.ID, "error": map[string]any{"code": "invalid_request"}})
					continue
				}
				_ = conn.WriteJSON(map[string]any{"id": msg.ID, "result": map[string]any{"ok": true}})
			default:
				_ = conn.WriteJSON(map[string]any{"id": msg.ID, "error": map[string]any{"code": "unsupported"}})
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	instance, err := cymonkey.Adapter{}.Connect(context.Background(), manifest.EngineSpec{Adapter: "cymonkey"}, orchestrator.EngineTarget{
		Kind:      "godot",
		Endpoints: []orchestrator.TargetEndpoint{{Name: "websocket", Protocol: "websocket", URL: wsURL}},
	})
	if err != nil {
		t.Fatalf("native connect failed: %v", err)
	}
	defer instance.Disconnect(context.Background())

	if provider, ok := instance.(orchestrator.EngineCapabilityProvider); !ok {
		t.Fatal("instance does not expose engine capabilities")
	} else if got := provider.EngineCapabilities(); len(got) != 1 || got[0] != "object.visible.set" {
		t.Fatalf("capabilities = %#v", got)
	}

	caller := instance.(bridge.Caller)
	helloRes, err := caller.Call(context.Background(), "hello", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(helloRes), `"websocket"`) {
		t.Fatalf("expected native hello passthrough, got %s", helloRes)
	}
	actRes, err := caller.Call(context.Background(), "act", json.RawMessage(`{"name":"object.visible.set","input":{"targetId":"object:door","visible":false}}`))
	if err != nil {
		t.Fatalf("native act failed: %v", err)
	}
	if !strings.Contains(string(actRes), `"ok"`) {
		t.Fatalf("unexpected act result %s", actRes)
	}

	rejected, err := caller.Call(context.Background(), "describe", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("describe passthrough failed: %v", err)
	}
	if !strings.Contains(string(rejected), "rev-7") {
		t.Fatalf("unexpected describe result %s", rejected)
	}
}
