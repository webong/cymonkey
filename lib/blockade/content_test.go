package blockade

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type contentAdapterFixture struct {
	capabilities []string
	request      ProviderAdapterInterpretRequest
	response     func(ProviderAdapterInterpretRequest) ProviderAdapterInterpretResponse
}

func (*contentAdapterFixture) Observe(context.Context, ProviderAdapterObserveRequest) (ProviderAdapterObserveResponse, error) {
	return ProviderAdapterObserveResponse{}, ErrModalityUnsupported
}
func (a *contentAdapterFixture) Interpret(_ context.Context, request ProviderAdapterInterpretRequest) (ProviderAdapterInterpretResponse, error) {
	a.request = request
	if a.response != nil {
		return a.response(request), nil
	}
	return ProviderAdapterInterpretResponse{APIVersion: ProviderAdapterAPIVersion,
		Response: InterpretResponse{APIVersion: ContentAPIVersion, RequestID: request.Request.RequestID,
			Findings: []ContentFinding{{Kind: "summary", Text: "a short summary", SourceParts: []int{0, 1}}}}}, nil
}
func (a *contentAdapterFixture) Capabilities(context.Context) (ProviderAdapterCapabilities, error) {
	return ProviderAdapterCapabilities{APIVersion: ProviderAdapterAPIVersion, Capabilities: a.capabilities}, nil
}
func (*contentAdapterFixture) Health(context.Context) (ProviderAdapterHealth, error) {
	return ProviderAdapterHealth{APIVersion: ProviderAdapterAPIVersion, Ready: true}, nil
}
func (*contentAdapterFixture) Close() error { return nil }

func startContentFixture(t *testing.T, adapter *contentAdapterFixture) Engine {
	t.Helper()
	registry := NewProviderAdapterRegistry()
	if err := registry.Register("fixture", func(context.Context, ProviderAdapterRuntime) (ProviderAdapter, error) {
		return adapter, nil
	}); err != nil {
		t.Fatal(err)
	}
	engine, err := StartConfiguredProviderAdapter(context.Background(), ProviderAdapterConfig{ID: "fixture", Provider: "fixture", MaxPayloadBytes: 1024}, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	return engine
}

func TestContentInterpretationAcrossHTTPAndProvider(t *testing.T) {
	adapter := &contentAdapterFixture{capabilities: []string{CapabilityMultimodalInterpret}}
	engine := startContentFixture(t, adapter)
	server, err := NewHTTPServer(engine, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Routes())
	defer httpServer.Close()
	request := InterpretRequest{RequestID: "request-1", Prompt: "Summarize and transcribe", Parts: []ContentPart{
		{Kind: "text", Text: "Meeting notes"},
		{Kind: "audio", MediaType: "audio/wav", Data: []byte("audio bytes")},
	}}
	result, err := (Client{BaseURL: httpServer.URL}).Interpret(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateInterpretResponse(result, request); err != nil {
		t.Fatal(err)
	}
	if adapter.request.APIVersion != ProviderAdapterAPIVersion || adapter.request.Request.APIVersion != ContentAPIVersion || !reflect.DeepEqual(adapter.request.Request.Parts, request.Parts) {
		t.Fatalf("provider request = %#v", adapter.request)
	}
	capabilities, err := engine.(EngineCapabilities).Capabilities(context.Background())
	if err != nil || !reflect.DeepEqual(capabilities, []string{CapabilityMultimodalInterpret}) {
		t.Fatalf("capabilities = %v, %v", capabilities, err)
	}
	image := httptest.NewRecorder()
	server.Routes().ServeHTTP(image, httptest.NewRequest(http.MethodPost, "/v1/observe", strings.NewReader(`{"image":"cG5n"}`)))
	if image.Code != http.StatusNotImplemented {
		t.Fatalf("content-only provider image status = %d", image.Code)
	}
}

func TestContentInterpretationRejectsUnsupportedAndInvalidOutput(t *testing.T) {
	adapter := &contentAdapterFixture{capabilities: []string{CapabilityTextInterpret}}
	engine := startContentFixture(t, adapter)
	request := InterpretRequest{Parts: []ContentPart{{Kind: "audio", MediaType: "audio/wav", Data: []byte("sound")}}}
	if _, err := engine.(ContentEngine).Interpret(context.Background(), request); !errors.Is(err, ErrModalityUnsupported) {
		t.Fatalf("audio without capability = %v", err)
	}
	adapter.response = func(request ProviderAdapterInterpretRequest) ProviderAdapterInterpretResponse {
		return ProviderAdapterInterpretResponse{APIVersion: ProviderAdapterAPIVersion,
			Response: InterpretResponse{APIVersion: ContentAPIVersion, RequestID: request.Request.RequestID,
				Findings: []ContentFinding{{Kind: "summary", Text: "bad", SourceParts: []int{2}}}}}
	}
	_, err := engine.(ContentEngine).Interpret(context.Background(), InterpretRequest{Parts: []ContentPart{{Kind: "text", Text: "hello"}}})
	if kind, ok := ProviderAdapterErrorKindOf(err); !ok || kind != ProviderAdapterErrorInvalidResponse {
		t.Fatalf("invalid source index = %v", err)
	}
}

func TestContentRequestValidation(t *testing.T) {
	for _, request := range []InterpretRequest{
		{APIVersion: ContentAPIVersion, Parts: []ContentPart{{Kind: "text", Text: " "}}},
		{APIVersion: ContentAPIVersion, Parts: []ContentPart{{Kind: "audio", Data: []byte("sound")}}},
		{APIVersion: ContentAPIVersion, Parts: []ContentPart{{Kind: "image", Text: "not bytes", MediaType: "image/png"}}},
	} {
		if err := ValidateInterpretRequest(request); err == nil {
			t.Fatalf("accepted invalid content request: %#v", request)
		}
	}
	server, err := NewHTTPServer(serverEngineFixture{}, "image-only")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/interpret", strings.NewReader(`{"parts":[{"kind":"text","text":"hello"}]}`))
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, request)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("image-only interpretation status = %d, body = %s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["error"] == "" {
		t.Fatalf("unsupported response = %s, %v", response.Body.String(), err)
	}
}
