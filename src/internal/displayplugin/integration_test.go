package displayplugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	"github.com/gorilla/websocket"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"jangolova/displaykit"
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
	pluginManifest, err = pluginManifest.WithReverse([]plugin.Contract{displaykit.HostContract()})
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

// recordingPolicy is a host policy that records what the engine asked and can
// refuse an endpoint with a message that contains a secret.
type recordingPolicy struct {
	mu           sync.Mutex
	validated    []string
	redacted     int
	environments int
	refuse       string
}

func (p *recordingPolicy) ValidateEndpoint(endpoint orchestrator.TargetEndpoint) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.validated = append(p.validated, endpoint.Name)
	if p.refuse != "" {
		return errors.New(p.refuse)
	}
	return nil
}

func (p *recordingPolicy) WorkerEnvironment(_ orchestrator.TargetEndpoint, environment []string) ([]string, error) {
	p.mu.Lock()
	p.environments++
	p.mu.Unlock()
	return append(environment, "POLICY=applied"), nil
}

func (p *recordingPolicy) Redact(message string, _ orchestrator.EngineTarget) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.redacted++
	return strings.ReplaceAll(message, "SECRET", "[REDACTED]")
}

func browserTarget() orchestrator.EngineTarget {
	return orchestrator.EngineTarget{
		Kind:      "browser",
		Endpoints: []orchestrator.TargetEndpoint{{Name: "cdp", Protocol: "cdp", URL: "ws://127.0.0.1:9222/devtools/browser/x"}},
	}
}

func TestDisplayEngineAsksTheHostToValidateAndRedacts(t *testing.T) {
	policy := &recordingPolicy{refuse: "endpoint carries SECRET material"}
	adapter := Adapter{Installed: installDisplayEngine(t), Policy: policy}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := adapter.Connect(ctx, manifest.EngineSpec{Adapter: "display-engine"}, browserTarget())
	if err == nil {
		t.Fatal("an endpoint the host refused connected")
	}
	if strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("the host's refusal reached the caller unredacted: %v", err)
	}
	policy.mu.Lock()
	defer policy.mu.Unlock()
	if len(policy.validated) != 1 || policy.validated[0] != "cdp" || policy.redacted == 0 {
		t.Fatalf("host was asked validated=%v redacted=%d", policy.validated, policy.redacted)
	}
}

func TestDisplayEngineWithoutHostServicesCannotUseAuthenticatedTargets(t *testing.T) {
	adapter := Adapter{Installed: installDisplayEngine(t)}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := adapter.Connect(ctx, manifest.EngineSpec{Adapter: "display-engine"}, browserTarget()); err == nil {
		t.Fatal("a browser target connected with no host policy to ask")
	}
}

func TestReverseHandlerServesOnlyDeclaredOperationsForTheConnectedTarget(t *testing.T) {
	policy := &recordingPolicy{}
	target := browserTarget()
	options := reverseOptions(policy, target)
	ctx := context.Background()
	request := func(operation string, payload any) plugin.Request {
		encoded, _ := json.Marshal(payload)
		return plugin.Request{Contract: plugin.ContractRef{Name: displaykit.HostContractName, Version: displaykit.HostContractVersion}, Operation: operation, Payload: encoded}
	}
	known := displaykit.Endpoint{Name: "cdp", Protocol: "cdp", URL: target.Endpoints[0].URL}

	if err := options.Authorize(ctx, request("shell", nil)); err == nil {
		t.Fatal("an operation outside the contract was admitted")
	}
	other := request("redact", nil)
	other.Contract.Name = "another.host"
	if err := options.Authorize(ctx, other); err == nil {
		t.Fatal("another contract was admitted")
	}
	if _, err := options.Handle(ctx, request("validate_endpoint", displaykit.EndpointRequest{Endpoint: known})); err != nil {
		t.Fatalf("known endpoint: %v", err)
	}
	unknown := known
	unknown.URL = "ws://10.0.0.5:9222/"
	if _, err := options.Handle(ctx, request("validate_endpoint", displaykit.EndpointRequest{Endpoint: unknown})); err == nil {
		t.Fatal("an endpoint that is not part of the target was validated")
	}
	raw, err := options.Handle(ctx, request("worker_environment", displaykit.EnvironmentRequest{Endpoint: known, Environment: []string{"A=1"}}))
	var environment displaykit.EnvironmentResult
	if err != nil || json.Unmarshal(raw, &environment) != nil || len(environment.Environment) != 2 {
		t.Fatalf("worker environment = %s, %v", raw, err)
	}
	raw, err = options.Handle(ctx, request("redact", displaykit.RedactRequest{Message: "a SECRET b"}))
	var redacted displaykit.RedactResult
	if err != nil || json.Unmarshal(raw, &redacted) != nil || redacted.Message != "a [REDACTED] b" {
		t.Fatalf("redact = %s, %v", raw, err)
	}
	if reverseOptions(nil, target) != nil {
		t.Fatal("a host without a policy offered services")
	}
}

func TestDisplayEngineRunsABrowserSessionThroughHostPolicy(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("the browser worker needs Node.js")
	}
	_, file, _, _ := runtime.Caller(0)
	worker, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "..", "tests", "cymonkey-worker-fixture.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	policy := &recordingPolicy{}
	adapter := Adapter{Installed: installDisplayEngine(t), Policy: policy, Environment: []string{"CYMONKEY_WORKER=" + worker}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	target := orchestrator.EngineTarget{Kind: "browser", Endpoints: []orchestrator.TargetEndpoint{{
		Name: "control", Protocol: "cdp", URL: "wss://browser.remote.example/devtools/browser/42",
	}}}
	instance, err := adapter.Connect(ctx, manifest.EngineSpec{Adapter: "display-engine"}, target)
	if err != nil {
		t.Fatal(err)
	}
	health := instance.(orchestrator.EngineHealthProvider).EngineHealth(ctx)
	if health.Status != orchestrator.EngineHealthHealthy {
		t.Fatalf("health = %+v", health)
	}
	found := false
	for _, capability := range instance.(orchestrator.EngineCapabilityProvider).EngineCapabilities() {
		found = found || capability == "script.register"
	}
	if !found {
		t.Fatal("the worker's capability did not reach the host")
	}
	if err := instance.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	policy.mu.Lock()
	defer policy.mu.Unlock()
	if len(policy.validated) == 0 || policy.validated[0] != "control" {
		t.Fatalf("the host's endpoint policy was not consulted: %v", policy.validated)
	}
	if policy.environments == 0 {
		t.Fatal("the host did not prepare the worker environment")
	}
}

func TestRefusalMessagesAreBoundedAndKeepTheirCharacters(t *testing.T) {
	policy := &recordingPolicy{refuse: strings.Repeat("é", 5000)}
	target := browserTarget()
	options := reverseOptions(policy, target)
	known := displaykit.Endpoint{Name: "cdp", Protocol: "cdp", URL: target.Endpoints[0].URL}
	encoded, _ := json.Marshal(displaykit.EndpointRequest{Endpoint: known})
	_, err := options.Handle(context.Background(), plugin.Request{
		Contract:  plugin.ContractRef{Name: displaykit.HostContractName, Version: displaykit.HostContractVersion},
		Operation: "validate_endpoint", Payload: encoded,
	})
	var remote *plugin.RemoteError
	if !errors.As(err, &remote) || remote.Code != "policy" {
		t.Fatalf("refusal = %v", err)
	}
	if len(remote.Message) > maxRefusalBytes || !utf8.ValidString(remote.Message) || remote.Message == "" {
		t.Fatalf("refusal message is %d bytes, valid UTF-8 %v", len(remote.Message), utf8.ValidString(remote.Message))
	}
}
