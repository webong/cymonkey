package blockade

import (
	"contentkit"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeProvider is a minimal content provider for exercising the HTTP service.
type fakeProvider struct {
	observe func(context.Context, contentkit.ProviderAdapterObserveRequest) (contentkit.ProviderAdapterObserveResponse, error)
	ready   bool
	detail  string
}

func (p *fakeProvider) Observe(ctx context.Context, request contentkit.ProviderAdapterObserveRequest) (contentkit.ProviderAdapterObserveResponse, error) {
	return p.observe(ctx, request)
}

func (p *fakeProvider) Capabilities(context.Context) (contentkit.ProviderAdapterCapabilities, error) {
	return contentkit.ProviderAdapterCapabilities{
		APIVersion:   contentkit.ProviderAdapterAPIVersion,
		Capabilities: []string{contentkit.CapabilityObjectDetect, contentkit.CapabilityImageObserve},
	}, nil
}

func (p *fakeProvider) Health(context.Context) (contentkit.ProviderAdapterHealth, error) {
	return contentkit.ProviderAdapterHealth{APIVersion: contentkit.ProviderAdapterAPIVersion, Ready: p.ready, Detail: p.detail}, nil
}

func (p *fakeProvider) Close() error { return nil }

func startProvider(t *testing.T, config contentkit.ProviderAdapterConfig, provider *fakeProvider) contentkit.Engine {
	t.Helper()
	registry := contentkit.NewProviderAdapterRegistry()
	factory := func(context.Context, contentkit.ProviderAdapterRuntime) (contentkit.ProviderAdapter, error) {
		return provider, nil
	}
	if err := registry.Register(config.Provider, factory); err != nil {
		t.Fatal(err)
	}
	engine, err := contentkit.StartConfiguredProviderAdapter(context.Background(), config, registry, contentkit.EnvironmentSecretResolver{})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestProviderAdapterHTTPReportsCapabilitiesHealthAndTypedError(t *testing.T) {
	adapter := &fakeProvider{ready: true}
	adapter.observe = func(context.Context, contentkit.ProviderAdapterObserveRequest) (contentkit.ProviderAdapterObserveResponse, error) {
		err := contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorRateLimit, errors.New("quota"))
		err.RetryAfter = 1500 * time.Millisecond
		return contentkit.ProviderAdapterObserveResponse{}, err
	}
	engine := startProvider(t, contentkit.ProviderAdapterConfig{ID: "fixture", Provider: "fixture"}, adapter)
	defer engine.Close()
	server, err := NewHTTPServer(engine, "fixture")
	if err != nil {
		t.Fatal(err)
	}

	for path := range map[string]bool{"/healthz": true, "/capabilities": true} {
		recorder := httptest.NewRecorder()
		server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", path, recorder.Code, recorder.Body.String())
		}
	}

	image := base64.StdEncoding.EncodeToString([]byte("image"))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/observe", strings.NewReader(`{"requestId":"request-1","image":"`+image+`"}`))
	server.Routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Retry-After") != "2" {
		t.Fatalf("Retry-After = %q", recorder.Header().Get("Retry-After"))
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["kind"] != string(contentkit.ProviderAdapterErrorRateLimit) {
		t.Fatalf("body = %#v", body)
	}
}

func TestProviderAdapterHTTPHealthNotReady(t *testing.T) {
	adapter := &fakeProvider{ready: true}
	adapter.ready = false
	adapter.detail = "provider unavailable"
	engine := startProvider(t, contentkit.ProviderAdapterConfig{ID: "fixture", Provider: "fixture"}, adapter)
	defer engine.Close()
	server, err := NewHTTPServer(engine, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), adapter.detail) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
