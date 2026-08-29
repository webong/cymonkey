package cymonkey

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	contract "jangolova/internal/cymonkey"
	"jangolova/internal/manifest"
	"jangolova/internal/orchestrator"
)

func TestWindowsCooperativeBackendNegotiatesOwnerHelper(t *testing.T) {
	connected, err := (Adapter{}).Connect(context.Background(), manifest.EngineSpec{Options: json.RawMessage(`{"domain":"viewer"}`)}, orchestrator.EngineTarget{Kind: "windows-application"})
	if err != nil {
		t.Fatal(err)
	}
	defer connected.Disconnect(context.Background())
	launch := connected.(orchestrator.EngineCallerLaunchProvider).EngineCallerLaunch().Environment
	serveFakeWindowsHelper(t, launch)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := connected.(*macOSInstance).Call(ctx, "capabilities", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var capabilities []contract.Capability
	if err := json.Unmarshal(raw, &capabilities); err != nil || len(capabilities) != 2 {
		t.Fatalf("capabilities = %s, %v", raw, err)
	}
	if _, err := connected.(*macOSInstance).Call(ctx, "act", json.RawMessage(`{"name":"display.capture","input":{"surfaceId":"windows-viewer:42-99"}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsTargetSelectsViewerDomain(t *testing.T) {
	domain, err := resolveDomain("", orchestrator.EngineTarget{Kind: "windows-application"})
	if err != nil || domain != contract.DomainViewer {
		t.Fatalf("resolveDomain() = %q, %v", domain, err)
	}
	backend, err := selectBackend("auto", orchestrator.EngineTarget{Kind: "windows-application"})
	if err != nil || backend.Name() != BackendWindowsCooperative {
		t.Fatalf("selectBackend() = %v, %v", backend, err)
	}
}

func serveFakeWindowsHelper(t *testing.T, environment map[string]string) {
	t.Helper()
	header := http.Header{"Authorization": []string{"Bearer " + environment["JANGOLOVA_CYMONKEY_CONTROL_TOKEN"]}}
	connection, _, err := websocket.DefaultDialer.Dial(environment["JANGOLOVA_CYMONKEY_CONTROL_URL"], header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	go func() {
		for {
			var request struct {
				ID     uint64          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := connection.ReadJSON(&request); err != nil {
				return
			}
			var result any
			switch request.Method {
			case "hello":
				result = contract.Hello{ProtocolVersion: contract.ProtocolVersion, Implementation: contract.Implementation{Name: "fake-windows-helper"}, Domains: []contract.Domain{contract.DomainViewer}, Runtimes: []string{"windows-app", "windows-viewer"}, Drivers: []contract.Driver{"windows-win32", "windows-viewer"}}
			case "capabilities":
				result = []contract.Capability{
					{Name: "window.list", Domain: contract.DomainViewer, Runtime: "windows-app", Driver: "windows-win32", Support: contract.SupportMapped, Lifetime: contract.LifetimeAttachment, Persistence: contract.PersistenceSession, Effect: "read", InputSchema: objectSchema()},
					{Name: "display.capture", Domain: contract.DomainViewer, Runtime: "windows-viewer", Driver: "windows-viewer", Support: contract.SupportNative, Lifetime: contract.LifetimeAttachment, Persistence: contract.PersistenceEphemeral, Effect: "read", InputSchema: objectSchema("surfaceId")},
				}
			case "act":
				result = map[string]any{"ok": true}
			case "describe":
				result = map[string]any{"revision": "1", "surfaces": []any{}, "augmentations": []any{}}
			case "events":
				result = map[string]any{"events": []any{}, "cursor": "0"}
			}
			if err := connection.WriteJSON(map[string]any{"id": request.ID, "result": result}); err != nil {
				return
			}
		}
	}()
}
