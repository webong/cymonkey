package engineprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	"cymonkey/src/internal/targetconn"
)

type fakeEngineAdapter struct {
	mu           sync.Mutex
	target       orchestrator.EngineTarget
	spec         manifest.EngineSpec
	instance     *fakeEngineInstance
	instances    []*fakeEngineInstance
	capabilities []string
	callResult   string
	screenshot   bool
}

func (f *fakeEngineAdapter) Connect(
	_ context.Context,
	spec manifest.EngineSpec,
	target orchestrator.EngineTarget,
) (orchestrator.EngineInstance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spec = spec
	f.target = target
	f.instance = &fakeEngineInstance{events: make(chan orchestrator.EngineEvent, 4), callResult: f.callResult, screenshot: f.screenshot}
	f.instances = append(f.instances, f.instance)
	return f.instance, nil
}

func (f *fakeEngineAdapter) snapshot() (orchestrator.EngineTarget, manifest.EngineSpec, *fakeEngineInstance, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.target, f.spec, f.instance, len(f.instances)
}

func (f *fakeEngineAdapter) InspectEngine(context.Context) orchestrator.EngineInspection {
	return orchestrator.EngineInspection{Available: true, Capabilities: append([]string(nil), f.capabilities...)}
}

type fakeEngineInstance struct {
	mu           sync.Mutex
	disconnected bool
	events       chan orchestrator.EngineEvent
	once         sync.Once
	callResult   string
	screenshot   bool
}

func (f *fakeEngineInstance) Disconnect(context.Context) error {
	f.mu.Lock()
	f.disconnected = true
	f.mu.Unlock()
	f.once.Do(func() { close(f.events) })
	return nil
}

func (f *fakeEngineInstance) Authorize(_ context.Context, request orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	if strings.TrimSpace(request.Action) == "" {
		return orchestrator.AuthorizeDecision{Authorized: false}, errors.New("fake action name is required")
	}
	return orchestrator.AuthorizeDecision{Authorized: true}, nil
}

func (f *fakeEngineInstance) isDisconnected() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.disconnected
}

func (f *fakeEngineInstance) EngineEvents() <-chan orchestrator.EngineEvent { return f.events }
func (f *fakeEngineInstance) EngineCapabilities() []string                  { return []string{"describe", "act"} }
func (f *fakeEngineInstance) Call(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if f.screenshot && method == "act" && strings.Contains(string(params), "window.screenshot") {
		return json.RawMessage(`{"pngBase64":"cG5n"}`), nil
	}
	if f.callResult != "" {
		return json.Marshal(map[string]string{"value": f.callResult})
	}
	return json.Marshal(map[string]any{"method": method, "params": params})
}

type nilEngineAdapter struct{}

func (nilEngineAdapter) Connect(context.Context, manifest.EngineSpec, orchestrator.EngineTarget) (orchestrator.EngineInstance, error) {
	return nil, nil
}

type healthEngineAdapter struct{}
type healthEngineInstance struct{}

type launchEngineAdapter struct{}
type launchEngineInstance struct{ fakeEngineInstance }

func (launchEngineAdapter) Connect(context.Context, manifest.EngineSpec, orchestrator.EngineTarget) (orchestrator.EngineInstance, error) {
	return &launchEngineInstance{fakeEngineInstance: fakeEngineInstance{events: make(chan orchestrator.EngineEvent, 1)}}, nil
}

func (*launchEngineInstance) EngineCallerLaunch() orchestrator.CallerLaunch {
	return orchestrator.CallerLaunch{Environment: map[string]string{
		"JANGOLOVA_CYMONKEY_CONTROL_URL":   "ws://127.0.0.1:7394/bridge",
		"JANGOLOVA_CYMONKEY_CONTROL_TOKEN": "ephemeral-test-token",
	}}
}

func (healthEngineAdapter) Connect(context.Context, manifest.EngineSpec, orchestrator.EngineTarget) (orchestrator.EngineInstance, error) {
	return healthEngineInstance{}, nil
}
func (healthEngineInstance) Disconnect(context.Context) error { return nil }
func (healthEngineInstance) Authorize(_ context.Context, request orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	if strings.TrimSpace(request.Action) == "" {
		return orchestrator.AuthorizeDecision{Authorized: false}, errors.New("health fake action name is required")
	}
	return orchestrator.AuthorizeDecision{Authorized: true}, nil
}
func (healthEngineInstance) EngineHealth(context.Context) orchestrator.EngineHealth {
	return orchestrator.EngineHealth{Status: orchestrator.EngineHealthUnhealthy, Message: "fixture probe failed", ObservedAt: time.Now().UTC()}
}

type recoveringHealthAdapter struct {
	mu       sync.Mutex
	attempts int
}

type recoveringHealthInstance struct {
	healthy bool
}

func (f *recoveringHealthAdapter) Connect(context.Context, manifest.EngineSpec, orchestrator.EngineTarget) (orchestrator.EngineInstance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	return &recoveringHealthInstance{healthy: f.attempts > 1}, nil
}

func (f *recoveringHealthAdapter) attemptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}

func (*recoveringHealthInstance) Disconnect(context.Context) error { return nil }
func (*recoveringHealthInstance) Authorize(_ context.Context, request orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	if strings.TrimSpace(request.Action) == "" {
		return orchestrator.AuthorizeDecision{Authorized: false}, errors.New("recovering health fake action name is required")
	}
	return orchestrator.AuthorizeDecision{Authorized: true}, nil
}
func (f *recoveringHealthInstance) EngineHealth(context.Context) orchestrator.EngineHealth {
	status := orchestrator.EngineHealthUnhealthy
	if f.healthy {
		status = orchestrator.EngineHealthHealthy
	}
	return orchestrator.EngineHealth{Status: status, Message: "fixture health", ObservedAt: time.Now().UTC()}
}

type leakingEngineAdapter struct{}

func (leakingEngineAdapter) Connect(context.Context, manifest.EngineSpec, orchestrator.EngineTarget) (orchestrator.EngineInstance, error) {
	return nil, errors.New("remote handshake echoed Bearer resolved-secret")
}

type recoveringEngineAdapter struct {
	mu        sync.Mutex
	attempts  int
	targetIDs []string
	instances []*fakeEngineInstance
}

func (f *recoveringEngineAdapter) Connect(
	_ context.Context,
	_ manifest.EngineSpec,
	target orchestrator.EngineTarget,
) (orchestrator.EngineInstance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	f.targetIDs = append(f.targetIDs, target.TargetID)
	if f.attempts == 2 {
		return nil, errors.New("target relay is not ready")
	}
	instance := &fakeEngineInstance{events: make(chan orchestrator.EngineEvent, 4)}
	f.instances = append(f.instances, instance)
	return instance, nil
}

func (f *recoveringEngineAdapter) snapshot() (int, []string, []*fakeEngineInstance) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts, append([]string(nil), f.targetIDs...), append([]*fakeEngineInstance(nil), f.instances...)
}

func TestServiceReattachesFailedEngineWithoutSupervisingTarget(t *testing.T) {
	registry := orchestrator.NewRegistry()
	adapter := &recoveringEngineAdapter{}
	if err := registry.RegisterEngine("recovering", adapter); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	service.recoveryInitialBackoff = time.Millisecond
	service.recoveryMaximumBackoff = 2 * time.Millisecond

	response := performRequest(service.Routes(), http.MethodPost, "/v1/instances", `{
		"apiVersion":"interaction.engine/v1alpha1","instanceId":"recover-one",
		"engine":{"adapter":"recovering"},
		"target":{"apiVersion":"interaction.target/v1alpha1","targetId":"caller-target-42","kind":"unity"}
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("connect status = %d: %s", response.Code, response.Body.String())
	}
	_, _, instances := adapter.snapshot()
	instances[0].events <- orchestrator.EngineEvent{
		Type: "interaction.failed", Status: "failed", Message: "relay exited", OccurredAt: time.Now().UTC(),
	}

	deadline := time.Now().Add(time.Second)
	for {
		response = performRequest(service.Routes(), http.MethodGet, "/v1/instances/recover-one/events", "")
		var batch InstanceEventBatch
		if err := json.NewDecoder(response.Body).Decode(&batch); err != nil {
			t.Fatal(err)
		}
		if hasEventType(batch.Events, "instance.recovery.retrying") && hasEventType(batch.Events, "instance.recovered") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("recovery events not observed: %#v", batch.Events)
		}
		time.Sleep(time.Millisecond)
	}

	attempts, targetIDs, instances := adapter.snapshot()
	if attempts != 3 || len(instances) != 2 {
		t.Fatalf("connect attempts = %d, instances = %d", attempts, len(instances))
	}
	for _, targetID := range targetIDs {
		if targetID != "caller-target-42" {
			t.Fatalf("recovery changed target identity: %#v", targetIDs)
		}
	}
	if !instances[0].isDisconnected() {
		t.Fatal("failed attachment was not released")
	}
	response = performRequest(service.Routes(), http.MethodDelete, "/v1/instances/recover-one", "")
	if response.Code != http.StatusNoContent || !instances[1].isDisconnected() {
		t.Fatalf("disconnect status = %d, recovered attachment still active", response.Code)
	}
}

func TestServiceRedactsResolvedCredentialFromAdapterFailure(t *testing.T) {
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("leaking", leakingEngineAdapter{}); err != nil {
		t.Fatal(err)
	}
	released := 0
	resolver := targetconn.ResolverFunc(func(context.Context, targetconn.Request) (targetconn.Material, error) {
		return targetconn.Material{
			Headers:   map[string]string{"Authorization": "Bearer resolved-secret"},
			ExpiresAt: time.Now().Add(time.Minute),
			Release:   func(context.Context) error { released++; return nil },
		}, nil
	})
	service, err := NewService(registry, "test-token", WithTargetResolver(resolver))
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(service.Routes(), http.MethodPost, "/v1/instances", `{
		"apiVersion":"interaction.engine/v1alpha1","instanceId":"redaction-one",
		"engine":{"adapter":"leaking"},
		"target":{"kind":"browser","endpoints":[{"name":"control","protocol":"cdp","url":"wss://browser.example/control","credentialRef":"session"}]}
	}`)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "resolved-secret") || !strings.Contains(response.Body.String(), "[REDACTED]") {
		t.Fatalf("response was not redacted: %s", response.Body.String())
	}
	if released != 1 {
		t.Fatalf("release count = %d", released)
	}
}

func TestServiceRedactsResolvedCredentialFromSuccessfulResult(t *testing.T) {
	registry := orchestrator.NewRegistry()
	adapter := &fakeEngineAdapter{callResult: "Bearer resolved-secret"}
	if err := registry.RegisterEngine("echo", adapter); err != nil {
		t.Fatal(err)
	}
	resolver := targetconn.ResolverFunc(func(context.Context, targetconn.Request) (targetconn.Material, error) {
		return targetconn.Material{
			Headers:   map[string]string{"Authorization": "Bearer resolved-secret"},
			ExpiresAt: time.Now().Add(time.Minute),
		}, nil
	})
	service, err := NewService(registry, "test-token", WithTargetResolver(resolver))
	if err != nil {
		t.Fatal(err)
	}
	connected := performRequest(service.Routes(), http.MethodPost, "/v1/instances", `{
		"apiVersion":"interaction.engine/v1alpha1","instanceId":"redaction-result",
		"engine":{"adapter":"echo"},
		"target":{"kind":"browser","endpoints":[{"name":"control","protocol":"cdp","url":"wss://browser.example/control","credentialRef":"session"}]}
	}`)
	if connected.Code != http.StatusCreated {
		t.Fatalf("connect status = %d: %s", connected.Code, connected.Body.String())
	}
	response := performRequest(service.Routes(), http.MethodPost, "/v1/instances/redaction-result/call", `{"method":"describe","params":{}}`)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "resolved-secret") || !strings.Contains(response.Body.String(), "[REDACTED]") {
		t.Fatalf("response was not redacted: %d %s", response.Code, response.Body.String())
	}
}

func TestServiceConnectsCallsAndDisconnectsEngine(t *testing.T) {
	t.Parallel()
	registry := orchestrator.NewRegistry()
	adapter := &fakeEngineAdapter{}
	if err := registry.RegisterEngine("playwright", adapter); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	handler := service.Routes()

	body := `{
		"apiVersion":"interaction.engine/v1alpha1",
		"instanceId":"browser-one",
		"engine":{"adapter":"playwright"},
		"target":{"kind":"browser","endpoints":[{"name":"cdp","protocol":"cdp","url":"http://127.0.0.1:9222"}],"handles":{"native.window":"window-1234"}}
	}`
	response := performRequest(handler, http.MethodPost, "/v1/instances", body)
	if response.Code != http.StatusCreated {
		t.Fatalf("connect status = %d: %s", response.Code, response.Body.String())
	}
	var instance Instance
	if err := json.NewDecoder(response.Body).Decode(&instance); err != nil {
		t.Fatal(err)
	}
	if instance.InstanceID != "browser-one" || instance.Status != "connected" || len(instance.Capabilities) != 2 {
		t.Fatalf("instance = %#v", instance)
	}
	target, _, initialInstance, _ := adapter.snapshot()
	if len(target.Endpoints) != 1 || target.Endpoints[0].URL != "http://127.0.0.1:9222" {
		t.Fatalf("target = %#v", target)
	}
	if target.Handles["native.window"] != "window-1234" {
		t.Fatalf("handles = %#v", target.Handles)
	}

	response = performRequest(handler, http.MethodPost, "/v1/instances/browser-one/call", `{"method":"describe","params":{}}`)
	if response.Code != http.StatusOK {
		t.Fatalf("call status = %d: %s", response.Code, response.Body.String())
	}
	var call CallResponse
	if err := json.NewDecoder(response.Body).Decode(&call); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(call.Result), `"method":"describe"`) {
		t.Fatalf("call = %#v", call)
	}

	initialInstance.events <- orchestrator.EngineEvent{Type: "interaction.failed", Status: "failed", Message: "fixture exited", OccurredAt: time.Now().UTC()}
	deadline := time.Now().Add(time.Second)
	for {
		response = performRequest(handler, http.MethodGet, "/v1/instances/browser-one/events?after=2", "")
		var batch InstanceEventBatch
		if err := json.NewDecoder(response.Body).Decode(&batch); err != nil {
			t.Fatal(err)
		}
		if hasEventType(batch.Events, "interaction.failed") && hasEventType(batch.Events, "instance.recovered") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("terminal event not observed: %#v", batch)
		}
		time.Sleep(time.Millisecond)
	}

	response = performRequest(handler, http.MethodDelete, "/v1/instances/browser-one", "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("disconnect status = %d", response.Code)
	}
	_, _, recoveredInstance, attempts := adapter.snapshot()
	if attempts < 2 {
		t.Fatalf("connect attempts = %d, want recovery", attempts)
	}
	if !initialInstance.isDisconnected() || !recoveredInstance.isDisconnected() {
		t.Fatal("interaction engine was not disconnected")
	}
}

func TestServiceRequiresAuthorization(t *testing.T) {
	t.Parallel()
	service, err := NewService(orchestrator.NewRegistry(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/engines", nil)
	response := httptest.NewRecorder()
	service.Routes().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestServiceReturnsCallerLaunchOnlyOnInitialConnection(t *testing.T) {
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("launch-fixture", launchEngineAdapter{}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(service.Routes(), http.MethodPost, "/v1/instances", `{
		"apiVersion":"interaction.engine/v1alpha1","instanceId":"launch-one",
		"engine":{"adapter":"launch-fixture"},"target":{"kind":"macos-application"}
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("connect status = %d: %s", response.Code, response.Body.String())
	}
	var connected Instance
	if err := json.NewDecoder(response.Body).Decode(&connected); err != nil {
		t.Fatal(err)
	}
	if connected.CallerLaunch == nil || connected.CallerLaunch.Environment["JANGOLOVA_CYMONKEY_CONTROL_TOKEN"] != "ephemeral-test-token" {
		t.Fatalf("caller launch = %#v", connected.CallerLaunch)
	}

	response = performRequest(service.Routes(), http.MethodGet, "/v1/instances/launch-one", "")
	describedBody := response.Body.Bytes()
	var described Instance
	if err := json.Unmarshal(describedBody, &described); err != nil {
		t.Fatal(err)
	}
	if described.CallerLaunch != nil || strings.Contains(string(describedBody), "ephemeral-test-token") {
		t.Fatalf("caller launch persisted in instance description: %s", describedBody)
	}
}

func TestServiceAutomaticallySelectsEngineFromCallerSuppliedTarget(t *testing.T) {
	registry := orchestrator.NewRegistry()
	playwright := &fakeEngineAdapter{capabilities: []string{"target.cdp", "window.evaluate"}}
	presentation := &fakeEngineAdapter{capabilities: []string{"target.cdp", "presentation.mount"}}
	if err := registry.RegisterEngine("playwright", playwright); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterEngine("web-presentation", presentation); err != nil {
		t.Fatal(err)
	}
	releaseCount := 0
	resolver := targetconn.ResolverFunc(func(_ context.Context, request targetconn.Request) (targetconn.Material, error) {
		switch request.Kind {
		case targetconn.CredentialReference:
			return targetconn.Material{
				Headers:   map[string]string{"Authorization": "Bearer resolved-secret"},
				ExpiresAt: time.Now().Add(time.Hour),
				Release:   func(context.Context) error { releaseCount++; return nil },
			}, nil
		case targetconn.TLSReference:
			return targetconn.Material{TLS: &orchestrator.TLSConnection{CAFile: "/caller/ca.pem"}}, nil
		default:
			return targetconn.Material{}, targetconn.ErrReferenceNotFound
		}
	})
	service, err := NewService(registry, "test-token", WithTargetResolver(resolver))
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(service.Routes(), http.MethodPost, "/v1/instances", `{
		"apiVersion":"interaction.engine/v1alpha1",
		"instanceId":"remote-presentation",
		"engine":{"adapter":"auto","requiredCapabilities":["presentation.mount"]},
		"target":{
			"apiVersion":"interaction.target/v1alpha1",
			"targetId":"remote-browser-42",
			"kind":"browser",
			"endpoints":[{
				"name":"browser-control",
				"protocol":"cdp",
				"url":"wss://browser.example/control/42",
				"credentialRef":"browser-session-42",
				"tlsRef":"browser-cluster-ca",
				"audience":"engine",
				"metadata":{"network.scope":"private"}
			}],
			"metadata":{"owner.kind":"external"}
		}
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("connect status = %d: %s", response.Code, response.Body.String())
	}
	var connected Instance
	if err := json.NewDecoder(response.Body).Decode(&connected); err != nil {
		t.Fatal(err)
	}
	if connected.Adapter != "web-presentation" {
		t.Fatalf("selected adapter = %q", connected.Adapter)
	}
	_, _, playwrightInstance, _ := playwright.snapshot()
	if playwrightInstance != nil {
		t.Fatal("non-matching engine was connected")
	}
	presentationTarget, presentationSpec, _, _ := presentation.snapshot()
	if presentationSpec.Adapter != "web-presentation" || len(presentationSpec.RequiredCapabilities) != 1 {
		t.Fatalf("selected spec = %#v", presentationSpec)
	}
	if presentationTarget.TargetID != "remote-browser-42" || presentationTarget.APIVersion != TargetAPIVersion {
		t.Fatalf("target identity = %#v", presentationTarget)
	}
	endpoint := presentationTarget.Endpoints[0]
	if endpoint.URL != "wss://browser.example/control/42" || endpoint.CredentialRef != "browser-session-42" || endpoint.TLSRef != "browser-cluster-ca" || endpoint.Metadata["network.scope"] != "private" {
		t.Fatalf("forwarded endpoint = %#v", endpoint)
	}
	if endpoint.Connection == nil || endpoint.Connection.Headers["Authorization"] != "Bearer resolved-secret" || endpoint.Connection.TLS.CAFile != "/caller/ca.pem" {
		t.Fatalf("resolved connection = %#v", endpoint.Connection)
	}
	response = performRequest(service.Routes(), http.MethodDelete, "/v1/instances/remote-presentation", "")
	if response.Code != http.StatusNoContent || releaseCount != 1 {
		t.Fatalf("disconnect status = %d, releases = %d", response.Code, releaseCount)
	}
}

func TestAutomaticSelectionDoesNotDependOnTargetLocation(t *testing.T) {
	registry := orchestrator.NewRegistry()
	adapter := &fakeEngineAdapter{capabilities: []string{"target.cdp", "window.evaluate"}}
	if err := registry.RegisterEngine("playwright", adapter); err != nil {
		t.Fatal(err)
	}
	for _, endpointURL := range []string{
		"http://127.0.0.1:9222",
		"http://chromium:9222",
		"http://10.30.0.14:9222",
		"wss://browser.remote.example/control",
	} {
		selected, err := SelectAutomaticEngine(context.Background(), registry, Target{
			Kind:      "browser",
			Endpoints: []TargetEndpoint{{Name: "control", Protocol: "cdp", URL: endpointURL}},
		}, nil)
		if err != nil {
			t.Fatalf("select %s: %v", endpointURL, err)
		}
		if selected != "playwright" {
			t.Fatalf("select %s = %q", endpointURL, selected)
		}
	}
}

func TestServiceRejectsEmptyEngineInstance(t *testing.T) {
	t.Parallel()
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("empty", nilEngineAdapter{}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(service.Routes(), http.MethodPost, "/v1/instances", `{
		"apiVersion":"interaction.engine/v1alpha1","instanceId":"empty-one",
		"engine":{"adapter":"empty"},"target":{"kind":"browser"}
	}`)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("connect status = %d, want 502", response.Code)
	}
}

func TestServiceActivelyProbesInstanceHealth(t *testing.T) {
	t.Parallel()
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("health-fixture", healthEngineAdapter{}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(service.Routes(), http.MethodPost, "/v1/instances", `{
		"apiVersion":"interaction.engine/v1alpha1","instanceId":"health-one",
		"engine":{"adapter":"health-fixture"},"target":{"kind":"fixture"}
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("connect status = %d: %s", response.Code, response.Body.String())
	}
	response = performRequest(service.Routes(), http.MethodGet, "/v1/instances/health-one", "")
	var instance Instance
	if err := json.NewDecoder(response.Body).Decode(&instance); err != nil {
		t.Fatal(err)
	}
	if instance.Health.Status != orchestrator.EngineHealthUnhealthy || instance.Health.Message != "fixture probe failed" {
		t.Fatalf("health = %#v", instance.Health)
	}
}

func TestServiceRecoversAfterRepeatedUnhealthyProbes(t *testing.T) {
	registry := orchestrator.NewRegistry()
	adapter := &recoveringHealthAdapter{}
	if err := registry.RegisterEngine("health-recovery", adapter); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	service.recoveryInitialBackoff = time.Millisecond
	response := performRequest(service.Routes(), http.MethodPost, "/v1/instances", `{
		"apiVersion":"interaction.engine/v1alpha1","instanceId":"health-recovery-one",
		"engine":{"adapter":"health-recovery"},"target":{"kind":"browser"}
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("connect status = %d: %s", response.Code, response.Body.String())
	}
	for probe := 0; probe < 2; probe++ {
		response = performRequest(service.Routes(), http.MethodGet, "/v1/instances/health-recovery-one", "")
		if response.Code != http.StatusOK {
			t.Fatalf("health status = %d: %s", response.Code, response.Body.String())
		}
	}

	deadline := time.Now().Add(time.Second)
	for {
		response = performRequest(service.Routes(), http.MethodGet, "/v1/instances/health-recovery-one/events", "")
		var batch InstanceEventBatch
		if err := json.NewDecoder(response.Body).Decode(&batch); err != nil {
			t.Fatal(err)
		}
		if hasEventType(batch.Events, "instance.recovered") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("health recovery not observed: %#v", batch.Events)
		}
		time.Sleep(time.Millisecond)
	}
	if adapter.attemptCount() != 2 {
		t.Fatalf("connect attempts = %d", adapter.attemptCount())
	}
	response = performRequest(service.Routes(), http.MethodDelete, "/v1/instances/health-recovery-one", "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("disconnect status = %d", response.Code)
	}
}

func TestValidateConnectRequestRejectsInvalidTargets(t *testing.T) {
	t.Parallel()
	base := ConnectRequest{APIVersion: APIVersion, InstanceID: "engine-one", Engine: EngineSpec{Adapter: "playwright"}, Target: Target{Kind: "browser"}}
	tests := []ConnectRequest{base, base, base}
	tests[0].Target.Kind = ""
	tests[1].Target.Endpoints = []TargetEndpoint{{Name: "cdp/path", Protocol: "cdp", URL: "http://localhost:9222"}}
	tests[2].Target.Handles = map[string]string{"native.window": ""}
	for index, request := range tests {
		if err := validateConnectRequest(request); err == nil {
			t.Fatalf("invalid target %d was accepted", index)
		}
	}
}

func TestEventQueryValidation(t *testing.T) {
	t.Parallel()
	if cursor, err := parseCursor("12"); err != nil || cursor != 12 {
		t.Fatalf("parseCursor() = %d, %v", cursor, err)
	}
	if _, err := parseCursor("later"); err == nil {
		t.Fatal("invalid cursor was accepted")
	}
	if limit, err := parseEventLimit("256"); err != nil || limit != 256 {
		t.Fatalf("parseEventLimit() = %d, %v", limit, err)
	}
	if _, err := parseEventLimit("257"); err == nil {
		t.Fatal("oversized limit was accepted")
	}
}

func performRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestServiceRequiresOneTimeCoreApprovalForConfiguredAction(t *testing.T) {
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("fake", &fakeEngineAdapter{}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	handler := service.Routes()
	response := performRequest(handler, http.MethodPost, "/v1/instances", `{
  "apiVersion":"interaction.engine/v1alpha1","instanceId":"approval-one",
  "engine":{"adapter":"fake","approval":{"requiredActions":["object.visible.set"]}},
  "target":{"kind":"godot"}
}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("connect status = %d: %s", response.Code, response.Body.String())
	}
	params := `{"name":"object.visible.set","input":{"id":"object:door","visible":false}}`
	response = performRequest(handler, http.MethodPost, "/v1/instances/approval-one/call", `{"method":"act","params":`+params+`}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unapproved action status = %d: %s", response.Code, response.Body.String())
	}
	response = performRequest(handler, http.MethodPost, "/v1/instances/approval-one/approvals", `{"params":`+params+`}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("request approval status = %d: %s", response.Code, response.Body.String())
	}
	var approval Approval
	if err := json.NewDecoder(response.Body).Decode(&approval); err != nil {
		t.Fatal(err)
	}
	response = performRequest(handler, http.MethodPost, "/v1/instances/approval-one/approvals/"+approval.ApprovalID, `{"approved":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("approve status = %d: %s", response.Code, response.Body.String())
	}
	response = performRequest(handler, http.MethodPost, "/v1/instances/approval-one/call", `{"method":"act","approvalId":"`+approval.ApprovalID+`","params":`+params+`}`)
	if response.Code != http.StatusOK {
		t.Fatalf("approved action status = %d: %s", response.Code, response.Body.String())
	}
	response = performRequest(handler, http.MethodPost, "/v1/instances/approval-one/call", `{"method":"act","approvalId":"`+approval.ApprovalID+`","params":`+params+`}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("reused approval status = %d: %s", response.Code, response.Body.String())
	}
	response = performRequest(handler, http.MethodGet, "/v1/instances/approval-one/events", "")
	var events InstanceEventBatch
	if err := json.NewDecoder(response.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"action.requested", "action.approval_requested", "action.approval_approved", "action.approval_consumed", "action.completed"} {
		if !hasEventType(events.Events, expected) {
			t.Fatalf("missing audit event %q in %#v", expected, events.Events)
		}
	}
}

func hasEventType(events []InstanceEvent, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func TestServiceReconcileCreatesMissingAndRetainsExisting(t *testing.T) {
	t.Parallel()
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("playwright", &fakeEngineAdapter{}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	handler := service.Routes()

	response := performRequest(handler, http.MethodPost, "/v1/instances", `{
		"apiVersion":"interaction.engine/v1alpha1","instanceId":"browser-one",
		"engine":{"adapter":"playwright"},"target":{"kind":"browser"}
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("connect status = %d: %s", response.Code, response.Body.String())
	}

	body := `{
		"apiVersion":"interaction.engine/v1alpha1","prune":false,"desired":[
			{"apiVersion":"interaction.engine/v1alpha1","instanceId":"browser-one","engine":{"adapter":"playwright"},"target":{"kind":"browser"}},
			{"apiVersion":"interaction.engine/v1alpha1","instanceId":"browser-two","engine":{"adapter":"playwright"},"target":{"kind":"browser"}}
		]
	}`
	response = performRequest(handler, http.MethodPost, "/v1/reconcile", body)
	if response.Code != http.StatusOK {
		t.Fatalf("reconcile status = %d: %s", response.Code, response.Body.String())
	}
	var result ReconcileResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Reconciled != 2 {
		t.Fatalf("reconciled = %d, want 2", result.Reconciled)
	}
	if !containsString(result.Retained, "browser-one") {
		t.Fatalf("retained = %#v, want browser-one", result.Retained)
	}
	if !containsString(result.Created, "browser-two") {
		t.Fatalf("created = %#v, want browser-two", result.Created)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("failed = %#v, want none", result.Failed)
	}

	response = performRequest(handler, http.MethodGet, "/v1/instances/browser-two", "")
	var instance Instance
	if err := json.NewDecoder(response.Body).Decode(&instance); err != nil {
		t.Fatal(err)
	}
	if instance.InstanceID != "browser-two" || instance.Status != "connected" {
		t.Fatalf("instance = %#v", instance)
	}
}

func TestServiceReconcilePrunesStaleInstances(t *testing.T) {
	t.Parallel()
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("playwright", &fakeEngineAdapter{}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	handler := service.Routes()

	for _, id := range []string{"keep-one", "drop-one"} {
		response := performRequest(handler, http.MethodPost, "/v1/instances", `{
			"apiVersion":"interaction.engine/v1alpha1","instanceId":"`+id+`",
			"engine":{"adapter":"playwright"},"target":{"kind":"browser"}
		}`)
		if response.Code != http.StatusCreated {
			t.Fatalf("connect %s status = %d: %s", id, response.Code, response.Body.String())
		}
	}

	body := `{
		"apiVersion":"interaction.engine/v1alpha1","prune":true,"desired":[
			{"apiVersion":"interaction.engine/v1alpha1","instanceId":"keep-one","engine":{"adapter":"playwright"},"target":{"kind":"browser"}}
		]
	}`
	response := performRequest(handler, http.MethodPost, "/v1/reconcile", body)
	if response.Code != http.StatusOK {
		t.Fatalf("reconcile status = %d: %s", response.Code, response.Body.String())
	}
	var result ReconcileResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Reconciled != 1 {
		t.Fatalf("reconciled = %d, want 1", result.Reconciled)
	}
	if !containsString(result.Retained, "keep-one") {
		t.Fatalf("retained = %#v, want keep-one", result.Retained)
	}
	if !containsString(result.Pruned, "drop-one") {
		t.Fatalf("pruned = %#v, want drop-one", result.Pruned)
	}

	response = performRequest(handler, http.MethodGet, "/v1/instances/drop-one", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("pruned instance status = %d, want 404", response.Code)
	}
	response = performRequest(handler, http.MethodGet, "/v1/instances/keep-one", "")
	if response.Code != http.StatusOK {
		t.Fatalf("kept instance status = %d, want 200", response.Code)
	}
}

func TestServiceReconcileCollectsPerInstanceFailures(t *testing.T) {
	t.Parallel()
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("playwright", &fakeEngineAdapter{}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}

	body := `{
		"apiVersion":"interaction.engine/v1alpha1","prune":false,"desired":[
			{"apiVersion":"interaction.engine/v1alpha1","instanceId":"valid-one","engine":{"adapter":"playwright"},"target":{"kind":"browser"}},
			{"apiVersion":"interaction.engine/v1alpha1","instanceId":"missing-kind","engine":{"adapter":"playwright"}},
			{"apiVersion":"interaction.engine/v1alpha1","instanceId":"unknown-adapter","engine":{"adapter":"nope"},"target":{"kind":"browser"}},
			{"apiVersion":"interaction.engine/v1alpha1","instanceId":"unknown-adapter","engine":{"adapter":"nope"},"target":{"kind":"browser"}}
		]
	}`
	response := performRequest(service.Routes(), http.MethodPost, "/v1/reconcile", body)
	if response.Code != http.StatusOK {
		t.Fatalf("reconcile status = %d: %s", response.Code, response.Body.String())
	}
	var result ReconcileResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !containsString(result.Created, "valid-one") {
		t.Fatalf("created = %#v, want valid-one", result.Created)
	}
	if _, ok := result.Failed["missing-kind"]; !ok {
		t.Fatalf("failed = %#v, want missing-kind recorded", result.Failed)
	}
	if _, ok := result.Failed["unknown-adapter"]; !ok {
		t.Fatalf("failed = %#v, want unknown-adapter recorded", result.Failed)
	}
	if result.Reconciled != 1 {
		t.Fatalf("reconciled = %d, want 1", result.Reconciled)
	}
}

func TestServiceReconcileRejectsMalformedRequest(t *testing.T) {
	t.Parallel()
	service, err := NewService(orchestrator.NewRegistry(), "test-token")
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(service.Routes(), http.MethodPost, "/v1/reconcile", `{"apiVersion":`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
	}
}

func TestServiceReconcileRebuildsAfterRestart(t *testing.T) {
	t.Parallel()
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("playwright", &fakeEngineAdapter{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterEngine("puppeteer", &fakeEngineAdapter{}); err != nil {
		t.Fatal(err)
	}

	// Simulate initial provider run: connect instances directly.
	first, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"apiVersion":"interaction.engine/v1alpha1","instanceId":"browser-one","engine":{"adapter":"playwright"},"target":{"kind":"browser"}}`,
		`{"apiVersion":"interaction.engine/v1alpha1","instanceId":"browser-two","engine":{"adapter":"playwright"},"target":{"kind":"browser"}}`,
		`{"apiVersion":"interaction.engine/v1alpha1","instanceId":"firefox-one","engine":{"adapter":"puppeteer"},"target":{"kind":"browser"}}`,
	} {
		response := performRequest(first.Routes(), http.MethodPost, "/v1/instances", body)
		if response.Code != http.StatusCreated {
			t.Fatalf("initial connect status = %d: %s", response.Code, response.Body.String())
		}
	}

	// Simulate provider restart: discard the service, create a fresh one against
	// the same registry (caller-owned targets and adapters survive the restart).
	second, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}

	// Caller submits its desired-state manifest. The new provider should create
	// the two desired instances from its clean state (the old instances from
	// first were lost on restart, which is why reconcile is needed).
	body := `{
		"apiVersion":"interaction.engine/v1alpha1","prune":true,"desired":[
			{"apiVersion":"interaction.engine/v1alpha1","instanceId":"browser-one","engine":{"adapter":"playwright"},"target":{"kind":"browser"}},
			{"apiVersion":"interaction.engine/v1alpha1","instanceId":"firefox-one","engine":{"adapter":"puppeteer"},"target":{"kind":"browser"}}
		]
	}`
	response := performRequest(second.Routes(), http.MethodPost, "/v1/reconcile", body)
	if response.Code != http.StatusOK {
		t.Fatalf("reconcile status = %d: %s", response.Code, response.Body.String())
	}
	var result ReconcileResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Reconciled != 2 {
		t.Fatalf("reconciled = %d, want 2", result.Reconciled)
	}
	if !containsString(result.Created, "browser-one") {
		t.Fatalf("created = %#v, want browser-one", result.Created)
	}
	if !containsString(result.Created, "firefox-one") {
		t.Fatalf("created = %#v, want firefox-one", result.Created)
	}
	if len(result.Pruned) != 0 {
		t.Fatalf("pruned = %#v, want none after restart (no stale instances)", result.Pruned)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("failed = %#v, want none", result.Failed)
	}

	// Verify the rebuilt instances are functional.
	for _, id := range []string{"browser-one", "firefox-one"} {
		response := performRequest(second.Routes(), http.MethodGet, "/v1/instances/"+id, "")
		if response.Code != http.StatusOK {
			t.Fatalf("rebuilt %s status = %d: %s", id, response.Code, response.Body.String())
		}
		var instance Instance
		if err := json.NewDecoder(response.Body).Decode(&instance); err != nil {
			t.Fatal(err)
		}
		if instance.Status != "connected" {
			t.Fatalf("rebuilt %s status = %q", id, instance.Status)
		}
	}
	response = performRequest(second.Routes(), http.MethodGet, "/v1/instances/browser-two", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("browser-two not recreated status = %d, want 404", response.Code)
	}
}
func TestServiceEnforcesPolicyGateOnActCalls(t *testing.T) {
	t.Parallel()
	registry := orchestrator.NewRegistry()
	adapter := &fakeEngineAdapter{}
	if err := registry.RegisterEngine("playwright", adapter); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	handler := service.Routes()

	// Connect an instance.
	response := performRequest(handler, http.MethodPost, "/v1/instances", `{
		"apiVersion":"interaction.engine/v1alpha1",
		"instanceId":"browser-one",
		"engine":{"adapter":"playwright"},
		"target":{"kind":"browser","endpoints":[{"name":"cdp","protocol":"cdp","url":"http://127.0.0.1:9222"}]}
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("connect status = %d: %s", response.Code, response.Body.String())
	}

	// Act call with empty action name should be rejected by the policy gate.
	response = performRequest(handler, http.MethodPost, "/v1/instances/browser-one/call", `{"method":"act","params":{"name":""}}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("empty action status = %d, want 403: %s", response.Code, response.Body.String())
	}

	// Act call with a valid action name should pass through to Call.
	response = performRequest(handler, http.MethodPost, "/v1/instances/browser-one/call", `{"method":"act","params":{"name":"describe"}}`)
	if response.Code != http.StatusOK {
		t.Fatalf("valid action status = %d, want 200: %s", response.Code, response.Body.String())
	}

	// Non-act methods (e.g. "hello") should pass through without authorization.
	response = performRequest(handler, http.MethodPost, "/v1/instances/browser-one/call", `{"method":"hello","params":{}}`)
	if response.Code != http.StatusOK {
		t.Fatalf("non-act method status = %d, want 200: %s", response.Code, response.Body.String())
	}
}
