package blockadewebllm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"blockade"
)

func newProtocolTestAdapter(state string) *Adapter {
	_, cancel := context.WithCancel(context.Background())
	return &Adapter{
		config: adapterConfig{
			model: "Phi-3.5-vision-instruct-q4f16_1-MLC", moduleURL: defaultModuleURL,
			maxTokens: defaultMaxTokens, contextWindowSize: defaultContextWindowSize,
		},
		token: "test-token", cancel: cancel,
		done: make(chan struct{}), state: state, stateChanged: make(chan struct{}),
		pending: make(map[string]*runtimeCall), queue: make(chan *runtimeCall),
	}
}

func TestRuntimeBridgeRoundTrip(t *testing.T) {
	adapter := newProtocolTestAdapter("ready")
	request := blockade.ProviderAdapterObserveRequest{
		APIVersion: blockade.ProviderAdapterAPIVersion,
		Request: blockade.ObserveRequest{
			APIVersion: blockade.APIVersion, RequestID: "observation-1",
			Image: []byte("png-bytes"), Prompt: "find controls",
		},
	}
	resultChannel := make(chan runtimeResult, 1)
	go func() {
		result, err := adapter.Observe(context.Background(), request)
		resultChannel <- runtimeResult{Response: result, Err: err}
	}()

	nextRecorder := httptest.NewRecorder()
	adapter.routes().ServeHTTP(nextRecorder, httptest.NewRequest(http.MethodGet, "/next?token=test-token", nil))
	if nextRecorder.Code != http.StatusOK {
		t.Fatalf("next status = %d, body = %s", nextRecorder.Code, nextRecorder.Body.String())
	}
	var call runtimeCall
	if err := json.Unmarshal(nextRecorder.Body.Bytes(), &call); err != nil {
		t.Fatal(err)
	}
	if call.Request.Request.RequestID != request.Request.RequestID || string(call.Request.Request.Image) != "png-bytes" {
		t.Fatalf("runtime call = %#v", call)
	}

	completion := runtimeCompletion{
		ID: call.ID,
		Result: &blockade.ProviderAdapterObserveResponse{
			APIVersion: blockade.ProviderAdapterAPIVersion,
			Response: blockade.ObserveResponse{
				APIVersion: blockade.APIVersion, RequestID: request.Request.RequestID,
				Observations: []blockade.Observation{{
					Kind: "control", Label: "submit", Confidence: 0.91,
					Region:   blockade.Region{X: 10, Y: 20, Width: 80, Height: 30},
					Evidence: "webllm:fixture",
				}},
			},
		},
	}
	document, err := json.Marshal(completion)
	if err != nil {
		t.Fatal(err)
	}
	completeRecorder := httptest.NewRecorder()
	adapter.routes().ServeHTTP(completeRecorder, httptest.NewRequest(http.MethodPost, "/complete?token=test-token", bytes.NewReader(document)))
	if completeRecorder.Code != http.StatusNoContent {
		t.Fatalf("complete status = %d, body = %s", completeRecorder.Code, completeRecorder.Body.String())
	}

	select {
	case result := <-resultChannel:
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		if err := blockade.ValidateObserveResponse(result.Response.Response); err != nil {
			t.Fatal(err)
		}
		if result.Response.Response.Observations[0].Evidence != "webllm:fixture" {
			t.Fatalf("response = %#v", result.Response)
		}
	case <-time.After(time.Second):
		t.Fatal("adapter did not receive runtime completion")
	}
}

func TestRuntimeFailureRemainsTyped(t *testing.T) {
	adapter := newProtocolTestAdapter("ready")
	resultChannel := make(chan error, 1)
	go func() {
		_, err := adapter.Observe(context.Background(), blockade.ProviderAdapterObserveRequest{})
		resultChannel <- err
	}()
	nextRecorder := httptest.NewRecorder()
	adapter.routes().ServeHTTP(nextRecorder, httptest.NewRequest(http.MethodGet, "/next?token=test-token", nil))
	var call runtimeCall
	if err := json.Unmarshal(nextRecorder.Body.Bytes(), &call); err != nil {
		t.Fatal(err)
	}
	document, _ := json.Marshal(runtimeCompletion{ID: call.ID, Error: &runtimeFailure{
		Kind: blockade.ProviderAdapterErrorInvalidResponse, Message: "bad model JSON",
	}})
	completeRecorder := httptest.NewRecorder()
	adapter.routes().ServeHTTP(completeRecorder, httptest.NewRequest(http.MethodPost, "/complete?token=test-token", bytes.NewReader(document)))
	err := <-resultChannel
	if kind, ok := blockade.ProviderAdapterErrorKindOf(err); !ok || kind != blockade.ProviderAdapterErrorInvalidResponse {
		t.Fatalf("error = %v, kind = %q", err, kind)
	}
}

func TestCanceledRequestInterruptsTheBrowserCall(t *testing.T) {
	adapter := newProtocolTestAdapter("ready")
	ctx, cancel := context.WithCancel(context.Background())
	resultChannel := make(chan error, 1)
	go func() {
		_, err := adapter.Observe(ctx, blockade.ProviderAdapterObserveRequest{})
		resultChannel <- err
	}()
	nextRecorder := httptest.NewRecorder()
	adapter.routes().ServeHTTP(nextRecorder, httptest.NewRequest(http.MethodGet, "/next?token=test-token", nil))
	var delivered runtimeCall
	if err := json.Unmarshal(nextRecorder.Body.Bytes(), &delivered); err != nil {
		t.Fatal(err)
	}
	adapter.mu.Lock()
	call := adapter.pending[delivered.ID]
	adapter.mu.Unlock()
	if call == nil {
		t.Fatal("delivered call is not pending")
	}
	cancelRecorder := httptest.NewRecorder()
	cancelHandled := make(chan struct{})
	go func() {
		adapter.routes().ServeHTTP(cancelRecorder, httptest.NewRequest(http.MethodGet, "/cancel?id="+delivered.ID+"&token=test-token", nil))
		close(cancelHandled)
	}()
	cancel()
	select {
	case <-call.Canceled:
	case <-time.After(time.Second):
		t.Fatal("browser cancellation signal was not released")
	}
	if err := <-resultChannel; err == nil {
		t.Fatal("canceled observation unexpectedly succeeded")
	}
	select {
	case <-cancelHandled:
		if cancelRecorder.Code != http.StatusNoContent {
			t.Fatalf("cancel status = %d", cancelRecorder.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("browser cancellation request remained blocked")
	}
}

func TestRuntimeStatusControlsHealthAndReadiness(t *testing.T) {
	adapter := newProtocolTestAdapter("starting")
	health, err := adapter.Health(context.Background())
	if err != nil || health.Ready {
		t.Fatalf("initial health = %#v, err = %v", health, err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/status?token=test-token", strings.NewReader(`{"state":"ready","detail":"model cached"}`))
	adapter.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status update = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	health, err = adapter.Health(context.Background())
	if err != nil || !health.Ready || health.Detail != "model cached" {
		t.Fatalf("updated health = %#v, err = %v", health, err)
	}

	unauthorized := httptest.NewRecorder()
	adapter.routes().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/config", nil))
	if unauthorized.Code != http.StatusNotFound {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
}

func TestConfigDefaultsAndValidation(t *testing.T) {
	config, err := parseConfig(map[string]string{"model": "Phi-3.5-vision-instruct-q4f16_1-MLC"})
	if err != nil {
		t.Fatal(err)
	}
	if config.moduleURL != defaultModuleURL || !config.headless || config.maxTokens != defaultMaxTokens || config.contextWindowSize != defaultContextWindowSize {
		t.Fatalf("defaults = %#v", config)
	}
	for name, settings := range map[string]map[string]string{
		"missing model":    {},
		"unknown setting":  {"model": "fixture", "targetTab": "forbidden"},
		"insecure module":  {"model": "fixture", "moduleURL": "http://provider.invalid/webllm.js"},
		"invalid tokens":   {"model": "fixture", "maxTokens": "0"},
		"invalid context":  {"model": "fixture", "contextWindowSize": "33000"},
		"invalid headless": {"model": "fixture", "headless": "sometimes"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseConfig(settings); err == nil {
				t.Fatal("expected configuration to fail")
			}
		})
	}
	if got := safePathSegment(" ../vision:one "); got != "---vision-one" {
		t.Fatalf("safe cache segment = %q", got)
	}
}

func TestRuntimeUsesOfficialWorkerVisionAndStructuredAPIs(t *testing.T) {
	for _, marker := range []string{
		"@mlc-ai/web-llm@0.2.84",
		"WebWorkerMLCEngineHandler",
		"CreateWebWorkerMLCEngine",
		`type: "image_url"`,
		`type: "json_object"`,
		"interruptGenerate",
		"blockade.observation/v1alpha1",
	} {
		if !strings.Contains(defaultModuleURL+runtimeJavaScript, marker) {
			t.Fatalf("runtime is missing %q", marker)
		}
	}
}

func TestCapabilitiesReportVisionLanguage(t *testing.T) {
	adapter := newProtocolTestAdapter("ready")
	capabilities, err := adapter.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.APIVersion != blockade.ProviderAdapterAPIVersion || len(capabilities.Capabilities) != 1 || capabilities.Capabilities[0] != blockade.CapabilityVisionLanguage {
		t.Fatalf("capabilities = %#v", capabilities)
	}
}
