package blockade

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProviderAdapter struct {
	mu              sync.Mutex
	request         ProviderAdapterObserveRequest
	observe         func(context.Context, ProviderAdapterObserveRequest) (ProviderAdapterObserveResponse, error)
	capabilities    ProviderAdapterCapabilities
	capabilitiesErr error
	health          ProviderAdapterHealth
	healthErr       error
	closeCount      int
}

func (a *fakeProviderAdapter) Observe(ctx context.Context, request ProviderAdapterObserveRequest) (ProviderAdapterObserveResponse, error) {
	a.mu.Lock()
	a.request = request
	a.mu.Unlock()
	if a.observe != nil {
		return a.observe(ctx, request)
	}
	return validFakeProviderResponse(request), nil
}

func (a *fakeProviderAdapter) Capabilities(context.Context) (ProviderAdapterCapabilities, error) {
	return a.capabilities, a.capabilitiesErr
}

func (a *fakeProviderAdapter) Health(context.Context) (ProviderAdapterHealth, error) {
	return a.health, a.healthErr
}

func (a *fakeProviderAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closeCount++
	return nil
}

func validFakeProviderResponse(request ProviderAdapterObserveRequest) ProviderAdapterObserveResponse {
	return ProviderAdapterObserveResponse{
		APIVersion: ProviderAdapterAPIVersion,
		Response: ObserveResponse{
			APIVersion: APIVersion,
			RequestID:  request.Request.RequestID,
			Observations: []Observation{{
				Kind: "object", Label: "fixture", Confidence: 0.9,
				Region:   Region{X: 1, Y: 2, Width: 3, Height: 4},
				Evidence: "fixture:model",
			}},
		},
	}
}

func readyFakeProvider() *fakeProviderAdapter {
	return &fakeProviderAdapter{
		capabilities: ProviderAdapterCapabilities{
			APIVersion:   ProviderAdapterAPIVersion,
			Capabilities: []string{CapabilityObjectDetect, CapabilityImageObserve},
		},
		health: ProviderAdapterHealth{APIVersion: ProviderAdapterAPIVersion, Ready: true},
	}
}

func startFakeProvider(t *testing.T, config ProviderAdapterConfig, adapter *fakeProviderAdapter, factory func(context.Context, ProviderAdapterRuntime) (ProviderAdapter, error)) Engine {
	t.Helper()
	registry := NewProviderAdapterRegistry()
	if factory == nil {
		factory = func(context.Context, ProviderAdapterRuntime) (ProviderAdapter, error) { return adapter, nil }
	}
	if err := registry.Register(config.Kind, factory); err != nil {
		t.Fatal(err)
	}
	engine, err := StartConfiguredProviderAdapter(context.Background(), config, registry, EnvironmentSecretResolver{})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestProviderAdapterEndToEnd(t *testing.T) {
	const secretValue = "fixture-secret-value"
	t.Setenv("BLOCKADE_FIXTURE_TOKEN", secretValue)
	adapter := readyFakeProvider()
	config := ProviderAdapterConfig{
		ID: "hosted-fixture", Kind: "fixture", Timeout: "1s", MaxPayloadBytes: 1024,
		Settings: map[string]string{"model": "fixture-v1"},
		Secrets:  map[string]SecretReference{"apiToken": {Env: "BLOCKADE_FIXTURE_TOKEN"}},
	}
	var resolvedSecret, configuredModel string
	engine := startFakeProvider(t, config, adapter, func(ctx context.Context, runtime ProviderAdapterRuntime) (ProviderAdapter, error) {
		var err error
		resolvedSecret, err = runtime.Secrets.Resolve(ctx, "apiToken")
		configuredModel = runtime.Settings["model"]
		return adapter, err
	})
	defer engine.Close()

	request := ObserveRequest{RequestID: "request-1", Image: []byte("original-image"), Prompt: "find the control"}
	result, err := engine.Observe(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateObserveResponse(result); err != nil {
		t.Fatal(err)
	}
	if resolvedSecret != secretValue || configuredModel != "fixture-v1" {
		t.Fatalf("runtime configuration = secret %q, model %q", resolvedSecret, configuredModel)
	}
	adapter.mu.Lock()
	captured := adapter.request
	adapter.mu.Unlock()
	if captured.APIVersion != ProviderAdapterAPIVersion || captured.Request.APIVersion != APIVersion {
		t.Fatalf("adapter envelope = %#v", captured)
	}
	if captured.Request.RequestID != request.RequestID || captured.Request.Prompt != request.Prompt || string(captured.Request.Image) != string(request.Image) {
		t.Fatalf("captured request = %#v", captured.Request)
	}

	reporter := engine.(EngineCapabilities)
	capabilities, err := reporter.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantCapabilities := []string{CapabilityImageObserve, CapabilityObjectDetect}
	if fmt.Sprint(capabilities) != fmt.Sprint(wantCapabilities) {
		t.Fatalf("capabilities = %v, want %v", capabilities, wantCapabilities)
	}
	health, err := engine.(EngineHealthReporter).Health(context.Background())
	if err != nil || !health.Ready {
		t.Fatalf("health = %#v, err = %v", health, err)
	}

	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	if adapter.closeCount != 1 {
		t.Fatalf("close count = %d, want 1", adapter.closeCount)
	}
}

func TestProviderAdapterTypedFailures(t *testing.T) {
	for _, kind := range []ProviderAdapterErrorKind{
		ProviderAdapterErrorAuthentication,
		ProviderAdapterErrorRateLimit,
		ProviderAdapterErrorUnavailable,
	} {
		t.Run(string(kind), func(t *testing.T) {
			adapter := readyFakeProvider()
			adapter.observe = func(context.Context, ProviderAdapterObserveRequest) (ProviderAdapterObserveResponse, error) {
				return ProviderAdapterObserveResponse{}, NewProviderAdapterError(kind, errors.New("provider detail"))
			}
			engine := startFakeProvider(t, ProviderAdapterConfig{ID: "fixture", Kind: "fixture"}, adapter, nil)
			defer engine.Close()
			_, err := engine.Observe(context.Background(), ObserveRequest{RequestID: "request-1", Image: []byte("image")})
			if got, ok := ProviderAdapterErrorKindOf(err); !ok || got != kind {
				t.Fatalf("error = %v, kind = %q", err, got)
			}
		})
	}
}

func TestProviderAdapterTimeoutAndCancellation(t *testing.T) {
	blockingAdapter := func() *fakeProviderAdapter {
		adapter := readyFakeProvider()
		adapter.observe = func(ctx context.Context, _ ProviderAdapterObserveRequest) (ProviderAdapterObserveResponse, error) {
			<-ctx.Done()
			return ProviderAdapterObserveResponse{}, ctx.Err()
		}
		return adapter
	}

	t.Run("timeout", func(t *testing.T) {
		engine := startFakeProvider(t, ProviderAdapterConfig{ID: "fixture", Kind: "fixture", Timeout: "10ms"}, blockingAdapter(), nil)
		defer engine.Close()
		_, err := engine.Observe(context.Background(), ObserveRequest{Image: []byte("image")})
		if kind, ok := ProviderAdapterErrorKindOf(err); !ok || kind != ProviderAdapterErrorTimeout {
			t.Fatalf("error = %v, kind = %q", err, kind)
		}
	})

	t.Run("canceled", func(t *testing.T) {
		engine := startFakeProvider(t, ProviderAdapterConfig{ID: "fixture", Kind: "fixture", Timeout: "1s"}, blockingAdapter(), nil)
		defer engine.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := engine.Observe(ctx, ObserveRequest{Image: []byte("image")})
		if kind, ok := ProviderAdapterErrorKindOf(err); !ok || kind != ProviderAdapterErrorCanceled {
			t.Fatalf("error = %v, kind = %q", err, kind)
		}
	})
}

func TestProviderAdapterRejectsInvalidPayloadAndResponse(t *testing.T) {
	t.Run("payload", func(t *testing.T) {
		engine := startFakeProvider(t, ProviderAdapterConfig{ID: "fixture", Kind: "fixture", MaxPayloadBytes: 4}, readyFakeProvider(), nil)
		defer engine.Close()
		_, err := engine.Observe(context.Background(), ObserveRequest{Image: []byte("12"), Prompt: "345"})
		if kind, ok := ProviderAdapterErrorKindOf(err); !ok || kind != ProviderAdapterErrorInvalidRequest {
			t.Fatalf("error = %v, kind = %q", err, kind)
		}
	})

	t.Run("response", func(t *testing.T) {
		adapter := readyFakeProvider()
		adapter.observe = func(_ context.Context, request ProviderAdapterObserveRequest) (ProviderAdapterObserveResponse, error) {
			result := validFakeProviderResponse(request)
			result.Response.Observations[0].Confidence = 2
			return result, nil
		}
		engine := startFakeProvider(t, ProviderAdapterConfig{ID: "fixture", Kind: "fixture"}, adapter, nil)
		defer engine.Close()
		_, err := engine.Observe(context.Background(), ObserveRequest{RequestID: "request-1", Image: []byte("image")})
		if kind, ok := ProviderAdapterErrorKindOf(err); !ok || kind != ProviderAdapterErrorInvalidResponse {
			t.Fatalf("error = %v, kind = %q", err, kind)
		}
	})
}

func TestProviderAdapterSecretDoesNotAppearInError(t *testing.T) {
	const secretValue = "do-not-print-this-secret"
	t.Setenv("BLOCKADE_FIXTURE_TOKEN", secretValue)
	config := ProviderAdapterConfig{
		ID: "fixture", Kind: "fixture",
		Secrets: map[string]SecretReference{"apiToken": {Env: "BLOCKADE_FIXTURE_TOKEN"}},
	}
	registry := NewProviderAdapterRegistry()
	if err := registry.Register("fixture", func(ctx context.Context, runtime ProviderAdapterRuntime) (ProviderAdapter, error) {
		secret, err := runtime.Secrets.Resolve(ctx, "apiToken")
		if err != nil {
			return nil, err
		}
		return nil, NewProviderAdapterError(ProviderAdapterErrorAuthentication, fmt.Errorf("provider rejected %s", secret))
	}); err != nil {
		t.Fatal(err)
	}
	_, err := StartConfiguredProviderAdapter(context.Background(), config, registry, EnvironmentSecretResolver{})
	if err == nil || strings.Contains(err.Error(), secretValue) {
		t.Fatalf("unsafe error = %v", err)
	}
	if kind, ok := ProviderAdapterErrorKindOf(err); !ok || kind != ProviderAdapterErrorAuthentication {
		t.Fatalf("error = %v, kind = %q", err, kind)
	}
}

func TestProviderAdapterHTTPReportsCapabilitiesHealthAndTypedError(t *testing.T) {
	adapter := readyFakeProvider()
	adapter.observe = func(context.Context, ProviderAdapterObserveRequest) (ProviderAdapterObserveResponse, error) {
		return ProviderAdapterObserveResponse{}, NewProviderAdapterError(ProviderAdapterErrorRateLimit, errors.New("quota"))
	}
	engine := startFakeProvider(t, ProviderAdapterConfig{ID: "fixture", Kind: "fixture"}, adapter, nil)
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
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["kind"] != string(ProviderAdapterErrorRateLimit) {
		t.Fatalf("body = %#v", body)
	}
}

func TestProviderAdapterHTTPHealthNotReady(t *testing.T) {
	adapter := readyFakeProvider()
	adapter.health.Ready = false
	adapter.health.Detail = "provider unavailable"
	engine := startFakeProvider(t, ProviderAdapterConfig{ID: "fixture", Kind: "fixture"}, adapter, nil)
	defer engine.Close()
	server, err := NewHTTPServer(engine, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), adapter.health.Detail) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestEnvironmentSecretResolverHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (EnvironmentSecretResolver{}).ResolveSecret(ctx, "BLOCKADE_UNUSED")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestProviderAdapterTimeoutIsBounded(t *testing.T) {
	config := ProviderAdapterConfig{ID: "fixture", Kind: "fixture", Timeout: (maximumProviderAdapterTimeout + time.Second).String()}
	if err := config.validate(); err == nil {
		t.Fatal("expected excessive timeout to fail validation")
	}
}
