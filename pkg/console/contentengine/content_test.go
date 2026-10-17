package contentengine

import (
	"context"
	"encoding/json"
	"errors"
	"jangolova/contentkit"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type contentAdapterFixture struct {
	capabilities []string
	request      contentkit.ProviderAdapterInterpretRequest
	response     func(contentkit.ProviderAdapterInterpretRequest) contentkit.ProviderAdapterInterpretResponse
}

func (*contentAdapterFixture) Observe(context.Context, contentkit.ProviderAdapterObserveRequest) (contentkit.ProviderAdapterObserveResponse, error) {
	return contentkit.ProviderAdapterObserveResponse{}, contentkit.ErrModalityUnsupported
}
func (a *contentAdapterFixture) Interpret(_ context.Context, request contentkit.ProviderAdapterInterpretRequest) (contentkit.ProviderAdapterInterpretResponse, error) {
	a.request = request
	if a.response != nil {
		return a.response(request), nil
	}
	return contentkit.ProviderAdapterInterpretResponse{APIVersion: contentkit.ProviderAdapterAPIVersion,
		Response: contentkit.InterpretResponse{APIVersion: contentkit.ContentAPIVersion, RequestID: request.Request.RequestID,
			Findings: []contentkit.ContentFinding{{Kind: "summary", Text: "a short summary", SourceParts: []int{0, 1}}}}}, nil
}
func (a *contentAdapterFixture) Capabilities(context.Context) (contentkit.ProviderAdapterCapabilities, error) {
	return contentkit.ProviderAdapterCapabilities{APIVersion: contentkit.ProviderAdapterAPIVersion, Capabilities: a.capabilities}, nil
}
func (*contentAdapterFixture) Health(context.Context) (contentkit.ProviderAdapterHealth, error) {
	return contentkit.ProviderAdapterHealth{APIVersion: contentkit.ProviderAdapterAPIVersion, Ready: true}, nil
}
func (*contentAdapterFixture) Close() error { return nil }

func startContentFixture(t *testing.T, adapter *contentAdapterFixture) Engine {
	t.Helper()
	registry := NewProviderAdapterRegistry()
	if err := registry.Register("fixture", func(context.Context, contentkit.ProviderAdapterRuntime) (contentkit.ProviderAdapter, error) {
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
	adapter := &contentAdapterFixture{capabilities: []string{contentkit.CapabilityMultimodalInterpret}}
	engine := startContentFixture(t, adapter)
	server, err := NewHTTPServer(engine, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Routes())
	defer httpServer.Close()
	request := contentkit.InterpretRequest{RequestID: "request-1", Prompt: "Summarize and transcribe", Parts: []contentkit.ContentPart{
		{Kind: "text", Text: "Meeting notes"},
		{Kind: "audio", MediaType: "audio/wav", Data: []byte("audio bytes")},
	}}
	result, err := (Client{BaseURL: httpServer.URL}).Interpret(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := contentkit.ValidateInterpretResponse(result, request); err != nil {
		t.Fatal(err)
	}
	if adapter.request.APIVersion != contentkit.ProviderAdapterAPIVersion || adapter.request.Request.APIVersion != contentkit.ContentAPIVersion || !reflect.DeepEqual(adapter.request.Request.Parts, request.Parts) {
		t.Fatalf("provider request = %#v", adapter.request)
	}
	capabilities, err := engine.(EngineCapabilities).Capabilities(context.Background())
	if err != nil || !reflect.DeepEqual(capabilities, []string{contentkit.CapabilityMultimodalInterpret}) {
		t.Fatalf("capabilities = %v, %v", capabilities, err)
	}
	image := httptest.NewRecorder()
	server.Routes().ServeHTTP(image, httptest.NewRequest(http.MethodPost, "/v1/observe", strings.NewReader(`{"image":"cG5n"}`)))
	if image.Code != http.StatusNotImplemented {
		t.Fatalf("content-only provider image status = %d", image.Code)
	}
}

func TestContentInterpretationRejectsUnsupportedAndInvalidOutput(t *testing.T) {
	adapter := &contentAdapterFixture{capabilities: []string{contentkit.CapabilityTextInterpret}}
	engine := startContentFixture(t, adapter)
	request := contentkit.InterpretRequest{Parts: []contentkit.ContentPart{{Kind: "audio", MediaType: "audio/wav", Data: []byte("sound")}}}
	if _, err := engine.(contentkit.ContentEngine).Interpret(context.Background(), request); !errors.Is(err, contentkit.ErrModalityUnsupported) {
		t.Fatalf("audio without capability = %v", err)
	}
	adapter.response = func(request contentkit.ProviderAdapterInterpretRequest) contentkit.ProviderAdapterInterpretResponse {
		return contentkit.ProviderAdapterInterpretResponse{APIVersion: contentkit.ProviderAdapterAPIVersion,
			Response: contentkit.InterpretResponse{APIVersion: contentkit.ContentAPIVersion, RequestID: request.Request.RequestID,
				Findings: []contentkit.ContentFinding{{Kind: "summary", Text: "bad", SourceParts: []int{2}}}}}
	}
	_, err := engine.(contentkit.ContentEngine).Interpret(context.Background(), contentkit.InterpretRequest{Parts: []contentkit.ContentPart{{Kind: "text", Text: "hello"}}})
	if kind, ok := contentkit.ProviderAdapterErrorKindOf(err); !ok || kind != contentkit.ProviderAdapterErrorInvalidResponse {
		t.Fatalf("invalid source index = %v", err)
	}
}

func TestContentRequestValidation(t *testing.T) {
	for _, request := range []contentkit.InterpretRequest{
		{APIVersion: contentkit.ContentAPIVersion, Parts: []contentkit.ContentPart{{Kind: "text", Text: " "}}},
		{APIVersion: contentkit.ContentAPIVersion, Parts: []contentkit.ContentPart{{Kind: "audio", Data: []byte("sound")}}},
		{APIVersion: contentkit.ContentAPIVersion, Parts: []contentkit.ContentPart{{Kind: "image", Text: "not bytes", MediaType: "image/png"}}},
		{APIVersion: contentkit.ContentAPIVersion, Parts: []contentkit.ContentPart{{Kind: "video", MediaType: "application/octet-stream", Data: []byte("video")}}},
	} {
		if err := contentkit.ValidateInterpretRequest(request); err == nil {
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
