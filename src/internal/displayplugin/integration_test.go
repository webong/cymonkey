package displayplugin

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	"github.com/gorilla/websocket"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"jangolova/displaykit/bridge"
)

// installDisplayEngine builds Board's display engine program and installs it as
// a display.engine package.
func installDisplayEngine(t *testing.T) store.Installed {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test location")
	}
	program := filepath.Join(t.TempDir(), "program")
	build := exec.Command("go", "build", "-o", program, "board/cmd/display")
	build.Dir = filepath.Join(filepath.Dir(file), "..", "..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build display engine: %v\n%s", err, output)
	}
	data, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "provider"), data, 0o700); err != nil {
		t.Fatal(err)
	}
	operations := make([]plugin.Operation, 0, len(Operations))
	for _, operation := range Operations {
		operations = append(operations, plugin.Operation{Name: operation})
	}
	contract := plugin.Contract{ContractRef: plugin.ContractRef{Name: ContractName, Version: ContractVersion}, Operations: operations}
	pluginManifest, err := packagekit.ExecutableManifest("display-engine", "1.0.0", []plugin.Contract{contract}, filepath.Join(source, "provider"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteManifest(source, pluginManifest); err != nil {
		t.Fatal(err)
	}
	packages, err := store.New(t.TempDir(), store.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := packages.Install(source)
	if err != nil {
		t.Fatal(err)
	}
	return installed
}

// serveHelper plays the cooperative helper that the engine asks the target's
// owner to launch: it connects with the launch variables and answers the
// control protocol.
func serveHelper(t *testing.T, environment map[string]string) {
	t.Helper()
	header := http.Header{"Authorization": []string{"Bearer " + environment["CYMONKEY_CONTROL_TOKEN"]}}
	connection, _, err := websocket.DefaultDialer.Dial(environment["CYMONKEY_CONTROL_URL"], header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	go func() {
		for {
			var request struct {
				ID     uint64 `json:"id"`
				Method string `json:"method"`
			}
			if err := connection.ReadJSON(&request); err != nil {
				return
			}
			var result any
			switch request.Method {
			case "hello":
				result = map[string]any{
					"protocolVersion": environment["CYMONKEY_PROTOCOL"],
					"implementation":  map[string]string{"name": "fake-macos-helper"},
					"domains":         []string{"viewer"}, "runtimes": []string{"macos-app"},
					"drivers": []string{"macos-apple-events"},
				}
			case "capabilities":
				result = []map[string]any{{
					"name": "app.command.invoke", "domain": "viewer", "runtime": "macos-app", "driver": "macos-apple-events",
					"support": "mapped", "lifetime": "attachment", "persistence": "session", "effect": "external",
					"inputSchema": map[string]any{"type": "object", "required": []string{"surfaceId", "command"}, "additionalProperties": true},
				}}
			case "events":
				result = map[string]any{"events": []any{}, "cursor": "0"}
			default:
				result = map[string]any{}
			}
			if err := connection.WriteJSON(map[string]any{"id": request.ID, "result": result}); err != nil {
				return
			}
		}
	}()
}

func TestDisplayEngineProgramServesACooperativeSession(t *testing.T) {
	adapter := Adapter{Installed: installDisplayEngine(t)}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inspection := adapter.InspectEngine(ctx)
	if !inspection.Available || len(inspection.Capabilities) == 0 {
		t.Fatalf("inspection = %+v", inspection)
	}

	instance, err := adapter.Connect(ctx, manifest.EngineSpec{Adapter: "display-engine", Options: json.RawMessage(`{"domain":"viewer"}`)},
		orchestrator.EngineTarget{Kind: "macos-application"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Disconnect(context.Background())

	// The launch variables carry Cymonkey's names, not the library's.
	launch := instance.(orchestrator.EngineCallerLaunchProvider).EngineCallerLaunch().Environment
	for _, name := range []string{"CYMONKEY_CONTROL_URL", "CYMONKEY_CONTROL_TOKEN", "CYMONKEY_PROTOCOL"} {
		if launch[name] == "" {
			t.Fatalf("launch environment lacks %s: %v", name, launch)
		}
	}
	for name := range launch {
		if strings.HasPrefix(name, "JANGOLOVA_") {
			t.Fatalf("launch environment exposes %s", name)
		}
	}
	serveHelper(t, launch)

	raw, err := instance.(bridge.Caller).Call(ctx, "capabilities", json.RawMessage(`{}`))
	if err != nil || !strings.Contains(string(raw), "app.command.invoke") {
		t.Fatalf("capabilities through the plugin = %s, %v", raw, err)
	}

	select {
	case event := <-instance.(orchestrator.EngineEventSource).EngineEvents():
		if !strings.HasPrefix(event.Type, "cymonkey.") {
			t.Fatalf("event %q keeps the library's name", event.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event reached the host")
	}
	if health := instance.(orchestrator.EngineHealthProvider).EngineHealth(ctx); health.Status == "" {
		t.Fatalf("health = %+v", health)
	}
}

func TestDisplayEngineProgramRefusesTargetsThatNeedHostServices(t *testing.T) {
	adapter := Adapter{Installed: installDisplayEngine(t)}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := adapter.Connect(ctx, manifest.EngineSpec{Adapter: "display-engine"}, orchestrator.EngineTarget{
		Kind: "browser", Endpoints: []orchestrator.TargetEndpoint{{Name: "cdp", Protocol: "cdp", URL: "ws://127.0.0.1:9222/devtools/browser/x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "requires the host") {
		t.Fatalf("a browser target connected without host services: %v", err)
	}
}
