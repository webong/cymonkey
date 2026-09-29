package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"blockade"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	"cymonkey/src/internal/userscripts"
	"cymonkey/src/observation"
)

type operatorTestAdapter struct{}

func (operatorTestAdapter) Connect(context.Context, manifest.EngineSpec, orchestrator.EngineTarget) (orchestrator.EngineInstance, error) {
	return operatorTestInstance{}, nil
}

type operatorTestInstance struct{}

func (operatorTestInstance) Disconnect(context.Context) error { return nil }
func (operatorTestInstance) Authorize(context.Context, orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	return orchestrator.AuthorizeDecision{Authorized: true}, nil
}
func (operatorTestInstance) Call(_ context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
	if bytes.Contains(params, []byte("window.screenshot")) {
		return json.RawMessage(`{"pngBase64":"cG5n"}`), nil
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func TestOperatorKeepsBrowserSessionCallableBehindAuthentication(t *testing.T) {
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("fixture", operatorTestAdapter{}); err != nil {
		t.Fatal(err)
	}
	server, err := newOperatorServer(registry, "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	request := `{"instanceId":"browser-one","adapter":"fixture","endpoint":"cdp=http://127.0.0.1:9222"}`
	unauthorized := operatorTestRequest(server, "", http.MethodPost, "/v1/browser-sessions", request)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	connected := operatorTestRequest(server, "secret", http.MethodPost, "/v1/browser-sessions", request)
	if connected.Code != http.StatusCreated || !strings.Contains(connected.Body.String(), `"instanceId":"browser-one"`) {
		t.Fatalf("connect = %d %s", connected.Code, connected.Body.String())
	}
	called := operatorTestRequest(server, "secret", http.MethodPost, "/v1/instances/browser-one/call", `{"method":"act","params":{"name":"window.click","input":{"selector":"#go"}}}`)
	if called.Code != http.StatusOK || !strings.Contains(called.Body.String(), `"ok":true`) {
		t.Fatalf("call = %d %s", called.Code, called.Body.String())
	}
	image, err := server.captureLocal(context.Background(), observation.ObservationRequest{InstanceID: "browser-one"})
	if err != nil || string(image) != "png" {
		t.Fatalf("local capture = %q, %v", image, err)
	}
	if operatorTestRequest(server, "secret", http.MethodDelete, "/v1/instances/browser-one", "").Code != http.StatusNoContent {
		t.Fatal("session did not disconnect")
	}
}

func TestOperatorExposesUserscriptAndExtensionActions(t *testing.T) {
	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	if unauthorized := operatorTestRequest(server, "", http.MethodPost, "/v1/extensions/actions", `{"name":"extension.targets","input":{}}`); unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("extension action without token = %d", unauthorized.Code)
	}
	path := filepath.Join(t.TempDir(), "sample.user.js")
	if err := os.WriteFile(path, []byte("// ==UserScript==\n// @name Sample\n// @match https://example.com/*\n// ==/UserScript==\nconsole.log('sample')"), 0600); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"targetId": "test-profile", "sourcePath": path})
	installed := operatorTestRequest(server, "secret", http.MethodPost, "/v1/userscripts/install", string(payload))
	if installed.Code != http.StatusOK || !strings.Contains(installed.Body.String(), `"status":"stored"`) {
		t.Fatalf("userscript install = %d %s", installed.Code, installed.Body.String())
	}
	listed := operatorTestRequest(server, "secret", http.MethodPost, "/v1/userscripts/list", `{"targetId":"test-profile"}`)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"name":"Sample"`) {
		t.Fatalf("userscript list = %d %s", listed.Code, listed.Body.String())
	}
	capabilities := operatorTestRequest(server, "secret", http.MethodPost, "/v1/extensions/actions", `{"name":"extension.capabilities","input":{"target":{"browser":"firefox"}}}`)
	if capabilities.Code != http.StatusOK || !strings.Contains(capabilities.Body.String(), "firefox") {
		t.Fatalf("extension capabilities = %d %s", capabilities.Code, capabilities.Body.String())
	}
}

func TestOperatorObservesItsOwnBrowserSession(t *testing.T) {
	blockadeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/observe" {
			t.Fatalf("Blockade path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(blockade.ObserveResponse{APIVersion: blockade.APIVersion, RequestID: "observed"})
	}))
	defer blockadeServer.Close()
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("fixture", operatorTestAdapter{}); err != nil {
		t.Fatal(err)
	}
	server, err := newOperatorServer(registry, "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	server.blockadeURL = blockadeServer.URL
	connected := operatorTestRequest(server, "secret", http.MethodPost, "/v1/browser-sessions", `{"instanceId":"browser-one","adapter":"fixture","endpoint":"cdp=http://127.0.0.1:9222"}`)
	if connected.Code != http.StatusCreated {
		t.Fatalf("connect = %d %s", connected.Code, connected.Body.String())
	}
	observed := operatorTestRequest(server, "secret", http.MethodPost, "/v1/observations", `{"instanceId":"browser-one"}`)
	if observed.Code != http.StatusOK || !strings.Contains(observed.Body.String(), `"requestId":"observed"`) {
		t.Fatalf("observation = %d %s", observed.Code, observed.Body.String())
	}
}

func TestOperatorCLIUsesThePersistentAPI(t *testing.T) {
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("fixture", operatorTestAdapter{}); err != nil {
		t.Fatal(err)
	}
	server, err := newOperatorServer(registry, "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	httpServer := httptest.NewServer(server.Routes())
	defer httpServer.Close()
	t.Setenv("CYMONKEY_OPERATOR_TOKEN", "secret")
	var output bytes.Buffer
	if err := operatorClient("connect", []string{"--url", httpServer.URL, "--instance", "browser-two", "--adapter", "fixture", "--endpoint", "cdp=http://127.0.0.1:9222"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"instanceId":"browser-two"`) {
		t.Fatalf("connect output = %s", output.String())
	}
	output.Reset()
	if err := operatorClient("call", []string{"--url", httpServer.URL, "--instance", "browser-two", "--name", "window.click", "--input", `{"selector":"#go"}`}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"ok":true`) {
		t.Fatalf("call output = %s", output.String())
	}
	output.Reset()
	if err := operatorExtensionClient([]string{"--url", httpServer.URL, "--name", "extension.capabilities", "--input", `{"target":{"browser":"firefox"}}`}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "firefox") {
		t.Fatalf("extension output = %s", output.String())
	}
	path := filepath.Join(t.TempDir(), "sample.user.js")
	if err := os.WriteFile(path, []byte("// ==UserScript==\n// @name Sample\n// @match https://example.com/*\n// ==/UserScript==\nconsole.log('sample')"), 0600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := operatorUserscriptClient([]string{"install", "--url", httpServer.URL, "--target", "test-profile", "--source", path}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"status":"stored"`) {
		t.Fatalf("userscript output = %s", output.String())
	}
}

func operatorTestRequest(server *operatorServer, token, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	result := httptest.NewRecorder()
	server.Routes().ServeHTTP(result, request)
	return result
}

func TestOperatorForwardsObservationApproval(t *testing.T) {
	t.Setenv("CYMONKEY_OPERATOR_TOKEN", "secret")
	var received observationOperatorRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	if err := operatorClient("observe", []string{"--url", server.URL, "--instance", "browser-one", "--approval-id", "approved-screenshot", "--full-page"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if received.ApprovalID != "approved-screenshot" || !received.FullPage {
		t.Fatalf("CLI discarded observation options: %+v", received)
	}
}

type reviewRecoveryAdapter struct{ connected chan *reviewRecoveryInstance }

func (a *reviewRecoveryAdapter) Connect(context.Context, manifest.EngineSpec, orchestrator.EngineTarget) (orchestrator.EngineInstance, error) {
	i := &reviewRecoveryInstance{events: make(chan orchestrator.EngineEvent, 1)}
	a.connected <- i
	return i, nil
}

type reviewRecoveryInstance struct {
	operatorTestInstance
	events     chan orchestrator.EngineEvent
	registered atomic.Int32
	closed     atomic.Bool
}

func (i *reviewRecoveryInstance) EngineEvents() <-chan orchestrator.EngineEvent { return i.events }
func (i *reviewRecoveryInstance) Disconnect(context.Context) error {
	if i.closed.CompareAndSwap(false, true) {
		close(i.events)
	}
	return nil
}
func (i *reviewRecoveryInstance) Call(_ context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
	var action struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &action); err != nil {
		return nil, err
	}
	if action.Name == "script.register" {
		i.registered.Add(1)
	}
	return json.RawMessage(`{"ok":true}`), nil
}
func TestOperatorReplaysUserscriptsAfterEngineRecovery(t *testing.T) {
	store := t.TempDir()
	record := userscripts.Record{Target: "review-profile", ID: "sample", Name: "Sample", Matches: []string{"https://example.com/*"}, Enabled: true, Source: "console.log('sample')"}
	record.Revision = userscripts.Revision(record)
	if err := userscripts.Save(store, record, false); err != nil {
		t.Fatal(err)
	}
	adapter := &reviewRecoveryAdapter{connected: make(chan *reviewRecoveryInstance, 4)}
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("fixture", adapter); err != nil {
		t.Fatal(err)
	}
	server, err := newOperatorServer(registry, "secret", store, "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	connected := operatorTestRequest(server, "secret", http.MethodPost, "/v1/browser-sessions", `{"instanceId":"recover-one","adapter":"fixture","endpoint":"cdp=http://127.0.0.1:9222","userscriptsTarget":"review-profile"}`)
	if connected.Code != http.StatusCreated {
		t.Fatalf("connect: %d %s", connected.Code, connected.Body.String())
	}
	first := <-adapter.connected
	if first.registered.Load() != 1 {
		t.Fatalf("initial replay count: %d", first.registered.Load())
	}
	first.events <- orchestrator.EngineEvent{Type: "worker.exited", Status: "failed", OccurredAt: time.Now()}
	var second *reviewRecoveryInstance
	select {
	case second = <-adapter.connected:
	case <-time.After(2 * time.Second):
		t.Fatal("engine recovery did not run")
	}
	deadline := time.Now().Add(time.Second)
	for {
		result := operatorTestRequest(server, "secret", http.MethodGet, "/v1/instances/recover-one", "")
		var state struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(result.Body.Bytes(), &state)
		if state.Status == "connected" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recovered instance not connected")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := server.syncTargetUserscripts(context.Background(), "review-profile"); err != nil {
		t.Fatal(err)
	}
	if second.registered.Load() == 0 {
		t.Fatalf("recovered session has no userscripts; previous session's applied cache also suppresses explicit reconciliation")
	}
}
