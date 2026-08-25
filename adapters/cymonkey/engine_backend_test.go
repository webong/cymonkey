package cymonkey_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"jangolova/adapters/cymonkey"
	"jangolova/internal/bridge"
	"jangolova/internal/manifest"
	"jangolova/internal/orchestrator"
	"jangolova/internal/pacman"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func TestCymonkeyEngineBackendConnect(t *testing.T) {
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
					"result": pacman.Hello{
						ProtocolVersion: pacman.ProtocolVersion,
						Implementation: pacman.Implementation{
							Engine:  "godot",
							Name:    "godot-cymonkey-test",
							Version: "1.0.0",
						},
					},
				})
			case "capabilities":
				_ = conn.WriteJSON(map[string]any{
					"id": msg.ID,
					"result": []pacman.Capability{
						{
							Name:        "pacman.scene.update",
							Effect:      "write",
							TargetKinds: []pacman.ResourceKind{pacman.KindScene},
							InputSchema: json.RawMessage(`{}`),
						},
					},
				})
			case "describe":
				_ = conn.WriteJSON(map[string]any{
					"id": msg.ID,
					"result": pacman.Description{
						Revision: "rev-1",
						Resources: []pacman.Resource{
							{ID: "scene:main", Kind: pacman.KindScene, Label: "Main Scene"},
						},
					},
				})
			case "act":
				_ = conn.WriteJSON(map[string]any{
					"id":     msg.ID,
					"result": map[string]any{"updated": true},
				})
			default:
				_ = conn.WriteJSON(map[string]any{
					"id":    msg.ID,
					"error": map[string]any{"code": "unsupported", "message": "unsupported method"},
				})
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	adapter := cymonkey.Adapter{}
	target := orchestrator.EngineTarget{
		Kind: "godot",
		Endpoints: []orchestrator.TargetEndpoint{
			{Name: "websocket", Protocol: "pacman-ws", URL: wsURL},
		},
	}

	ctx := context.Background()
	instance, err := adapter.Connect(ctx, manifest.EngineSpec{Adapter: "cymonkey"}, target)
	if err != nil {
		t.Fatalf("Cymonkey engine backend connect failed: %v", err)
	}
	defer instance.Disconnect(ctx)

	caller, ok := instance.(bridge.Caller)
	if !ok {
		t.Fatal("instance does not implement bridge.Caller")
	}

	// Call hello
	helloRes, err := caller.Call(ctx, "hello", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("hello call failed: %v", err)
	}
	if !strings.Contains(string(helloRes), "jangolova.cymonkey/v1alpha2") {
		t.Errorf("expected v1alpha2 protocol, got: %s", helloRes)
	}

	// Call describe
	descRes, err := caller.Call(ctx, "describe", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("describe call failed: %v", err)
	}
	if !strings.Contains(string(descRes), "scene:main") {
		t.Errorf("expected surface scene:main, got: %s", descRes)
	}

	// Call act
	actRes, err := caller.Call(ctx, "act", json.RawMessage(`{"name":"pacman.scene.update","input":{"targetId":"scene:main","expectedRevision":"rev-1"}}`))
	if err != nil {
		t.Fatalf("act call failed: %v", err)
	}
	if !strings.Contains(string(actRes), "updated") {
		t.Errorf("expected act result, got: %s", actRes)
	}
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
						"protocolVersion":     "jangolova.cymonkey/v1alpha2",
						"compatibleProtocols": []string{"jangolova.pacman/v1alpha1"},
						"implementation":      map[string]any{"name": "godot-native", "version": "0.2.0"},
						"profiles":            []string{"engine"},
						"backends":            []string{"engine-godot"},
					},
				})
			case "capabilities":
				_ = conn.WriteJSON(map[string]any{
					"id": msg.ID,
					"result": []map[string]any{{
						"name":        "object.visible.set",
						"profile":     "engine",
						"backend":     "engine-godot",
						"support":     "native",
						"lifetime":    "attachment",
						"persistence": "session",
						"effect":      "write",
						"targetKinds": []string{"object"},
						"inputSchema": json.RawMessage(`{}`),
					}},
				})
			case "describe":
				_ = conn.WriteJSON(map[string]any{
					"id": msg.ID,
					"result": map[string]any{
						"revision": "rev-7",
						"surfaces": []map[string]any{{
							"id": "object:door", "profile": "engine", "kind": "object", "label": "Front door",
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
		Endpoints: []orchestrator.TargetEndpoint{{Name: "websocket", Protocol: "pacman-ws", URL: wsURL}},
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
	if !strings.Contains(string(helloRes), `"engine-godot"`) {
		t.Fatalf("expected native hello passthrough, got %s", helloRes)
	}
	actRes, err := caller.Call(context.Background(), "act", json.RawMessage(`{"name":"object.visible.set","input":{"targetId":"object:door","visible":false}}`))
	if err != nil {
		t.Fatalf("native act failed: %v", err)
	}
	if !strings.Contains(string(actRes), `"ok"`) {
		t.Fatalf("unexpected act result %s", actRes)
	}
}
