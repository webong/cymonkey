package jangolova

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	contract "jangolova/contract"
	"jangolova/sdk"
)

func TestMacOSCooperativeBackendHandshakesAndEnforcesBundlePolicy(t *testing.T) {
	spec := sdk.EngineSpec{Options: json.RawMessage(`{
		"domain":"viewer",
		"policy":{"allowedBundleIds":["com.example.Allowed"]}
	}`)}
	connected, err := (Adapter{Host: testHost()}).Connect(context.Background(), spec, sdk.EngineTarget{Kind: "macos-application"})
	if err != nil {
		t.Fatal(err)
	}
	defer connected.Disconnect(context.Background())
	launch := connected.(sdk.EngineCallerLaunchProvider).EngineCallerLaunch().Environment
	var actions atomic.Int32
	serveFakeMacOSHelper(t, launch, &actions)

	caller := connected.(*macOSInstance)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := caller.Call(ctx, "capabilities", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var capabilities []contract.Capability
	if err := json.Unmarshal(raw, &capabilities); err != nil || len(capabilities) != 1 || capabilities[0].Name != "app.command.invoke" {
		t.Fatalf("capabilities = %s, %v", raw, err)
	}
	if _, err := caller.Call(ctx, "act", json.RawMessage(`{
		"name":"app.command.invoke",
		"input":{"surfaceId":"macos:com.example.Denied:7","command":"play"}
	}`)); err == nil {
		t.Fatal("unallowlisted bundle action was accepted")
	}
	if _, err := caller.Call(ctx, "act", json.RawMessage(`{
		"name":"app.command.invoke",
		"input":{"surfaceId":"macos:com.example.Allowed:42","command":"play"}
	}`)); err != nil {
		t.Fatal(err)
	}
	if actions.Load() != 1 {
		t.Fatalf("native helper actions = %d", actions.Load())
	}
}

func TestMacOSSwiftHelperLive(t *testing.T) {
	executable := os.Getenv("JANGOLOVA_CYMONKEY_MACOS_HELPER")
	if executable == "" {
		t.Skip("set JANGOLOVA_CYMONKEY_MACOS_HELPER to the test-built Swift helper")
	}
	connected, err := (Adapter{Host: testHost()}).Connect(context.Background(), sdk.EngineSpec{Options: json.RawMessage(`{"domain":"viewer"}`)}, sdk.EngineTarget{Kind: "macos-application"})
	if err != nil {
		t.Fatal(err)
	}
	launch := connected.(sdk.EngineCallerLaunchProvider).EngineCallerLaunch().Environment
	configPath := filepath.Join(t.TempDir(), "helper.json")
	if err := os.WriteFile(configPath, []byte(`{
		"allowedBundleIds":["com.example.UnavailableFixture"],
		"appleEventCommands":[],
		"accessibility":{
			"allowedBundleIds":["com.example.UnavailableFixture"],
			"allowedWritableAttributes":[],"maxDepth":2,"maxResults":4,
			"promptForAccessibilityConsent":false
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable)
	command.Env = append(os.Environ(),
		"JANGOLOVA_CYMONKEY_CONTROL_URL="+launch["JANGOLOVA_CONTROL_URL"],
		"JANGOLOVA_CYMONKEY_CONTROL_TOKEN="+launch["JANGOLOVA_CONTROL_TOKEN"],
		"JANGOLOVA_CYMONKEY_PROTOCOL="+launch["JANGOLOVA_CONTROL_PROTOCOL"],
		"JANGOLOVA_CYMONKEY_CONFIG="+configPath,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = connected.Disconnect(context.Background())
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := connected.(*macOSInstance).Call(ctx, "hello", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var hello contract.Hello
	if err := json.Unmarshal(raw, &hello); err != nil || hello.ProtocolVersion != contract.ProtocolVersion || !containsDomain(hello.Domains, contract.DomainViewer) {
		t.Fatalf("hello = %s, %v", raw, err)
	}
}

func serveFakeMacOSHelper(t *testing.T, environment map[string]string, actions *atomic.Int32) {
	t.Helper()
	header := http.Header{"Authorization": []string{"Bearer " + environment["JANGOLOVA_CONTROL_TOKEN"]}}
	connection, _, err := websocket.DefaultDialer.Dial(environment["JANGOLOVA_CONTROL_URL"], header)
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
				result = contract.Hello{
					ProtocolVersion: contract.ProtocolVersion,
					Implementation:  contract.Implementation{Name: "fake-macos-helper"},
					Domains:         []contract.Domain{contract.DomainViewer},
					Runtimes:        []string{"macos-app"},
					Drivers:         []contract.Driver{contract.DriverMacOSAppleEvents},
				}
			case "capabilities":
				result = []contract.Capability{{
					Name: "app.command.invoke", Domain: contract.DomainViewer, Runtime: "macos-app",
					Driver: contract.DriverMacOSAppleEvents, Support: contract.SupportMapped,
					Lifetime: contract.LifetimeAttachment, Persistence: contract.PersistenceSession,
					Effect: "external", InputSchema: objectSchema("surfaceId", "command"),
				}}
			case "act":
				actions.Add(1)
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
