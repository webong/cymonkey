package contentkit

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestClientObserve(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/observe" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"apiVersion":"content.observation/v1","requestId":"req-1","observations":[{"kind":"object","label":"button","confidence":0.9,"region":{"x":1,"y":2,"width":3,"height":4}}]}`)), Header: make(http.Header)}, nil
	})
	result, err := (Client{BaseURL: "http://blockade.test", HTTPClient: &http.Client{Transport: transport}, BearerToken: "secret"}).Observe(context.Background(), ObserveRequest{
		RequestID: "req-1", Image: []byte("image"), Prompt: "find controls",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Observations) != 1 || result.Observations[0].Label != "button" {
		t.Fatalf("result = %#v", result)
	}
}

func TestClientObservePreservesProviderAdapterErrorKind(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Retry-After", "3")
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       io.NopCloser(strings.NewReader(`{"error":"Blockade provider adapter failed: rate_limit","kind":"rate_limit"}`)),
			Header:     header,
		}, nil
	})
	_, err := (Client{BaseURL: "http://blockade.test", HTTPClient: &http.Client{Transport: transport}}).Observe(context.Background(), ObserveRequest{Image: []byte("image")})
	if kind, ok := ProviderAdapterErrorKindOf(err); !ok || kind != ProviderAdapterErrorRateLimit {
		t.Fatalf("error = %v, kind = %q", err, kind)
	}
	var adapterError *ProviderAdapterError
	if !errors.As(err, &adapterError) || adapterError.RetryAfter != 3*time.Second {
		t.Fatalf("adapter error = %#v", adapterError)
	}
}

func TestClientReadsAuthenticatedServiceHealthAndCapabilities(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		body := `{"status":"ok","provider":"fixture"}`
		if request.URL.Path == "/capabilities" {
			body = `{"apiVersion":"content.observation/v1","provider":"fixture","capabilities":["text.interpret"]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	client := Client{BaseURL: "http://blockade.test", BearerToken: "secret", HTTPClient: &http.Client{Transport: transport}}
	health, err := client.Health(context.Background())
	if err != nil || health.Status != "ok" || health.Provider != "fixture" {
		t.Fatalf("health = %#v, %v", health, err)
	}
	capabilities, err := client.Capabilities(context.Background())
	if err != nil || len(capabilities.Capabilities) != 1 || capabilities.Capabilities[0] != CapabilityTextInterpret {
		t.Fatalf("capabilities = %#v, %v", capabilities, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestObserveRequestImageCanBeEncodedForWorker(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("image"))
	if encoded == "" || !strings.Contains(encoded, "aW1hZ2U") {
		t.Fatalf("unexpected image encoding %q", encoded)
	}
}
