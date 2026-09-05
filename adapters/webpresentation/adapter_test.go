package webpresentation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"cymonkey/internal/manifest"
	"cymonkey/internal/orchestrator"
)

func TestAdapterPassesResolvedHeadersToRemoteCDPWorker(t *testing.T) {
	worker, err := filepath.Abs("../../tests/connection-material-worker.mjs")
	if err != nil {
		t.Fatal(err)
	}
	options, _ := json.Marshal(map[string]any{"workerPath": worker})
	instance, err := (Adapter{}).Connect(context.Background(), manifest.EngineSpec{Options: options}, orchestrator.EngineTarget{
		Kind: "browser",
		Endpoints: []orchestrator.TargetEndpoint{{
			Name: "control", Protocol: "cdp", URL: "wss://browser.remote.example/devtools/browser/42",
			Connection: &orchestrator.EndpointConnection{
				Headers:   map[string]string{"Authorization": "Bearer fixture-secret"},
				ExpiresAt: time.Now().Add(time.Minute),
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterReconnectsWorkerWhenCredentialRotates(t *testing.T) {
	worker, err := filepath.Abs("../../tests/connection-material-worker.mjs")
	if err != nil {
		t.Fatal(err)
	}
	options, _ := json.Marshal(map[string]any{"workerPath": worker})
	connection := &orchestrator.EndpointConnection{
		Headers:   map[string]string{"Authorization": "Bearer fixture-secret"},
		ExpiresAt: time.Now().Add(time.Minute),
	}
	connected, err := (Adapter{}).Connect(context.Background(), manifest.EngineSpec{Options: options}, orchestrator.EngineTarget{
		Kind: "browser", Endpoints: []orchestrator.TargetEndpoint{{
			Name: "control", Protocol: "cdp", URL: "wss://browser.remote.example/devtools/browser/42", Connection: connection,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connection.ReplaceCredential(map[string]string{"Authorization": "Bearer rotated-secret"}, time.Now().Add(2*time.Minute))
	events := connected.(orchestrator.EngineEventSource).EngineEvents()
	deadline := time.After(5 * time.Second)
	sawRedactedFailure := false
	for {
		select {
		case event := <-events:
			switch event.Type {
			case "interaction.connection.renewal_failed":
				if strings.Contains(event.Message, "rotated-secret") || !strings.Contains(event.Message, "[REDACTED]") {
					t.Fatalf("renewal failure was not redacted: %#v", event)
				}
				sawRedactedFailure = true
			case "interaction.connection.renewed":
				if !sawRedactedFailure {
					t.Fatal("presentation reconnect did not exercise retry path")
				}
				if err := connected.Disconnect(context.Background()); err != nil {
					t.Fatal(err)
				}
				return
			}
		case <-deadline:
			t.Fatal("credential rotation did not reconnect the presentation worker")
		}
	}
}

func TestAdapterReplacesWorkerWhenTLSRotates(t *testing.T) {
	worker, err := filepath.Abs("../../tests/connection-material-worker.mjs")
	if err != nil {
		t.Fatal(err)
	}
	options, _ := json.Marshal(map[string]any{"workerPath": worker})
	connection := &orchestrator.EndpointConnection{
		Headers: map[string]string{"Authorization": "Bearer fixture-secret"}, ExpiresAt: time.Now().Add(time.Minute),
	}
	connected, err := (Adapter{}).Connect(context.Background(), manifest.EngineSpec{Options: options}, orchestrator.EngineTarget{
		Kind: "browser", Endpoints: []orchestrator.TargetEndpoint{{
			Name: "control", Protocol: "cdp", URL: "wss://browser.remote.example/devtools/browser/42", Connection: connection,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(t.TempDir(), "rotated-ca.pem")
	if err := os.WriteFile(caPath, []byte("fixture CA material"), 0o600); err != nil {
		t.Fatal(err)
	}
	revision := connection.ReplaceTLS(&orchestrator.TLSConnection{CAFile: caPath}, time.Now().Add(2*time.Minute))
	for {
		select {
		case event := <-connected.(orchestrator.EngineEventSource).EngineEvents():
			if event.Type == "presentation.connected" {
				continue
			}
			if event.Type != "interaction.connection.renewed" {
				t.Fatalf("TLS rotation event = %#v", event)
			}
			_, acknowledged := connection.Acknowledgements()
			if acknowledged < revision {
				t.Fatalf("acknowledged revision = %d, want at least %d", acknowledged, revision)
			}
			if err := connected.Disconnect(context.Background()); err != nil {
				t.Fatal(err)
			}
			return
		case <-time.After(5 * time.Second):
			t.Fatal("TLS rotation did not replace the presentation worker")
		}
	}
}

func TestAdapterRequiresCallerOwnedCDPBrowser(t *testing.T) {
	t.Parallel()
	adapter := Adapter{}
	for name, target := range map[string]orchestrator.EngineTarget{
		"wrong kind":  {Kind: "native"},
		"missing cdp": {Kind: "browser"},
		"invalid cdp": {Kind: "browser", Endpoints: []orchestrator.TargetEndpoint{{Name: "cdp", Protocol: "cdp", URL: "file:///browser"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := adapter.Connect(context.Background(), manifest.EngineSpec{}, target); err == nil {
				t.Fatal("Connect() error = nil")
			}
		})
	}
}

func TestDecodeOptionsRejectsRuntimeLaunchFields(t *testing.T) {
	t.Parallel()
	if _, err := decodeOptions(json.RawMessage(`{"browserExecutable":"chromium"}`)); err == nil {
		t.Fatal("runtime-launch option was accepted")
	}
}

func TestInspectionFindsRepositoryWorker(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	worker := filepath.Join(filepath.Dir(file), "..", "..", "scripts", "presentation-worker.mjs")
	t.Setenv("JANGOLOVA_PRESENTATION_WORKER", worker)
	inspection := (Adapter{}).InspectEngine(context.Background())
	if !inspection.Available {
		t.Fatalf("InspectEngine() = %#v", inspection)
	}
}

func TestCapabilityNamesExposePresentationContract(t *testing.T) {
	values := capabilityNames()
	for _, required := range []string{"presentation.create", "presentation.replace", "presentation.write", "presentation.mount", "artifact.kind.web.entrypoint", "artifact.transport.http", "artifact.transport.target-file", "presentation.execute", "presentation.patch", "presentation.describe", "presentation.capture", "presentation.activate"} {
		found := false
		for _, value := range values {
			if value == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("capabilityNames() missing %q: %v", required, values)
		}
	}
}

func TestConnectedCapabilitiesRetainCommonInteractionMethods(t *testing.T) {
	values := stableStrings(append(capabilityNames(), "presentation.write", "custom.presentation.action"))
	for _, required := range []string{"describe", "act", "events", "presentation.write", "custom.presentation.action"} {
		found := false
		for _, value := range values {
			if value == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("connected capabilities missing %q: %v", required, values)
		}
	}
}

func TestPresentationPolicyDefaultsAndOverrides(t *testing.T) {
	defaults, err := decodeOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.resolved.MaxHTMLBytes != 1024*1024 || defaults.resolved.MaxTotalBytes != 1536*1024 {
		t.Fatalf("default policy = %#v", defaults.resolved)
	}
	if !defaults.resolved.actionAuthorized("presentation.execute") || !defaults.resolved.actionAuthorized("presentation.capture") || !defaults.resolved.actionAuthorized("presentation.mount") {
		t.Fatalf("default sensitive action authorization = %#v", defaults.resolved.AuthorizedActions)
	}

	configured, err := decodeOptions(json.RawMessage(`{
		"policy": {
			"maxHTMLBytes": 16,
			"maxTotalBytes": 32,
			"allowedSourceOrigins": ["https://PRESENTATION.example/"],
			"allowedAssetOrigins": ["self", "https://ASSETS.example", "self"],
			"allowedArtifactTransports": ["http", "target-file", "http"],
			"authorizedActions": ["presentation.capture"],
			"executeTimeoutMillis": 250,
			"captureTimeoutMillis": 500,
			"mountTimeoutMillis": 750
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if configured.resolved.MaxHTMLBytes != 16 || configured.resolved.MaxTotalBytes != 32 {
		t.Fatalf("configured policy = %#v", configured.resolved)
	}
	if got := configured.resolved.AllowedSourceOrigins; len(got) != 1 || got[0] != "https://presentation.example" {
		t.Fatalf("source origins = %v", got)
	}
	if got := configured.resolved.AllowedAssetOrigins; len(got) != 2 || got[1] != "https://assets.example" {
		t.Fatalf("asset origins = %v", got)
	}
	if got := configured.resolved.AllowedArtifactTransports; len(got) != 2 || got[1] != "target-file" {
		t.Fatalf("artifact transports = %v", got)
	}
	if configured.resolved.actionAuthorized("presentation.execute") || !configured.resolved.actionAuthorized("presentation.capture") {
		t.Fatalf("authorized actions = %v", configured.resolved.AuthorizedActions)
	}
	if configured.resolved.ExecuteTimeoutMillis != 250 || configured.resolved.CaptureTimeoutMillis != 500 || configured.resolved.MountTimeoutMillis != 750 {
		t.Fatalf("timeouts = %#v", configured.resolved)
	}
}

func TestPresentationPolicyRejectsDisallowedSourceAndOversizedArtifact(t *testing.T) {
	policy, err := resolvePolicy(policyConfig{
		MaxHTMLBytes:         4,
		MaxTotalBytes:        8,
		AllowedSourceOrigins: []string{"https://presentation.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.validateSource("https://other.example/presentation"); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("validateSource() error = %v", err)
	}
	params := json.RawMessage(`{"name":"presentation.write","input":{"expectedStateRevision":"0","html":"12345"}}`)
	if err := policy.validateCall("act", params); err == nil || !strings.Contains(err.Error(), "presentation HTML") {
		t.Fatalf("validateCall() error = %v", err)
	}
}

func TestPresentationPolicyRejectsInvalidConfiguration(t *testing.T) {
	for _, raw := range []string{
		`{"policy":{"maxHTMLBytes":-1}}`,
		`{"policy":{"allowedSourceOrigins":["https://example.com/path"]}}`,
		`{"policy":{"allowedAssetOrigins":["javascript:"]}}`,
		`{"policy":{"authorizedActions":["presentation.delete"]}}`,
		`{"policy":{"executeTimeoutMillis":-1}}`,
		`{"policy":{"captureTimeoutMillis":120001}}`,
		`{"policy":{"mountTimeoutMillis":-1}}`,
		`{"policy":{"allowedArtifactTransports":["xallet-volume"]}}`,
	} {
		if _, err := decodeOptions(json.RawMessage(raw)); err == nil {
			t.Fatalf("decodeOptions(%s) error = nil", raw)
		}
	}
}

func TestPresentationArtifactMountUsesProviderNeutralAllowedLocation(t *testing.T) {
	policy, err := resolvePolicy(policyConfig{
		AllowedSourceOrigins:      []string{"http://127.0.0.1:8082"},
		AllowedArtifactTransports: []string{"http"},
	})
	if err != nil {
		t.Fatal(err)
	}
	params := json.RawMessage(`{
		"name":"presentation.mount",
		"input":{
			"expectedStateRevision":"1",
			"artifact":{
				"apiVersion":"interaction.presentation/v1alpha1",
				"artifactId":"experience-42",
				"revision":"sha256:abc123",
				"kind":"web.entrypoint",
				"locations":[
					{"transport":"provider-handle","uri":"artifact://experience-42"},
					{"transport":"http","uri":"http://127.0.0.1:8082/"}
				]
			}
		}
	}`)
	if err := policy.validateCall("act", params); err != nil {
		t.Fatalf("validateCall() error = %v", err)
	}
}

func TestPresentationArtifactMountRejectsDisallowedLocation(t *testing.T) {
	policy := defaultPresentationPolicy()
	params := json.RawMessage(`{
		"name":"presentation.mount",
		"input":{
			"expectedStateRevision":"0",
			"artifact":{
				"apiVersion":"interaction.presentation/v1alpha1",
				"artifactId":"experience-42",
				"revision":"v1",
				"kind":"web.entrypoint",
				"locations":[{"transport":"target-file","uri":"file:///presentations/experience/index.html"}]
			}
		}
	}`)
	if err := policy.validateCall("act", params); err == nil || !strings.Contains(err.Error(), "allowed transport") {
		t.Fatalf("validateCall() error = %v", err)
	}
}

func TestPresentationArtifactSchemaIsProviderNeutral(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "protocol", "presentation", "v1", "artifact.schema.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(contents) {
		t.Fatal("artifact schema is invalid JSON")
	}
	lower := strings.ToLower(string(contents))
	for _, forbidden := range []string{"jangolova", "xallet"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("provider-neutral artifact schema contains %q", forbidden)
		}
	}
}

func TestSensitivePresentationActionsAreAuthorizedByPolicy(t *testing.T) {
	policy, err := resolvePolicy(policyConfig{AuthorizedActions: []string{"presentation.capture"}})
	if err != nil {
		t.Fatal(err)
	}
	params := json.RawMessage(`{"name":"presentation.execute","input":{"code":"return 1"}}`)
	if err := policy.validateCall("act", params); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("validateCall() error = %v", err)
	}
}

func TestSensitivePresentationActionsEmitAuditEvents(t *testing.T) {
	instance, _ := testInstance(defaultPresentationPolicy())
	result, err := instance.Call(context.Background(), "act", json.RawMessage(`{"name":"presentation.execute","input":{"code":"return 1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"ok":true}` {
		t.Fatalf("result = %s", result)
	}
	assertAuditEvent(t, instance.events, "presentation.execute.requested")
	assertAuditEvent(t, instance.events, "presentation.execute.succeeded")
}

func TestDeniedSensitivePresentationActionEmitsAuditEvent(t *testing.T) {
	policy := defaultPresentationPolicy()
	policy.AuthorizedActions = []string{"presentation.capture"}
	instance, worker := testInstance(policy)
	_, err := instance.Call(context.Background(), "act", json.RawMessage(`{"name":"presentation.execute","input":{"code":"return 1"}}`))
	if err == nil {
		t.Fatal("Call() error = nil")
	}
	assertAuditEvent(t, instance.events, "presentation.execute.requested")
	assertAuditEvent(t, instance.events, "presentation.execute.denied")
	if worker.calls != 0 {
		t.Fatal("denied action was sent to worker")
	}
}

func TestSensitivePresentationActionTimeoutCancelsRequest(t *testing.T) {
	policy := defaultPresentationPolicy()
	policy.ExecuteTimeoutMillis = 1
	instance, worker := testInstance(policy)
	worker.call = func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	_, err := instance.Call(context.Background(), "act", json.RawMessage(`{"name":"presentation.execute","input":{"code":"while(true){}"}}`))
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("Call() error = %v", err)
	}
	assertAuditEvent(t, instance.events, "presentation.execute.requested")
	assertAuditEvent(t, instance.events, "presentation.execute.cancelled")
}

type testWorker struct {
	calls int
	call  func(context.Context, string, json.RawMessage) (json.RawMessage, error)
}

func (w *testWorker) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	w.calls++
	return w.call(ctx, method, params)
}
func (*testWorker) Disconnect(context.Context) error { return nil }
func (*testWorker) Done() <-chan struct{}            { return make(chan struct{}) }
func (*testWorker) WaitError() error                 { return nil }
func (*testWorker) Terminate()                       {}
func (*testWorker) StderrSuffix() string             { return "" }

func testInstance(policy presentationPolicy) (*instance, *testWorker) {
	worker := &testWorker{call: func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	}}
	return &instance{
		worker: worker, events: make(chan orchestrator.EngineEvent, 8), policy: policy,
	}, worker
}

func assertAuditEvent(t *testing.T, events <-chan orchestrator.EngineEvent, eventType string) {
	t.Helper()
	select {
	case event := <-events:
		if event.Type != eventType || !event.OccurredAt.After(time.Time{}) {
			t.Fatalf("audit event = %#v, want %q", event, eventType)
		}
	default:
		t.Fatalf("missing audit event %q", eventType)
	}
}
