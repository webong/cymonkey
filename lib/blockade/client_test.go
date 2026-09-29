package blockade

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClientObserve(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/observe" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"apiVersion":"blockade.observation/v1alpha1","requestId":"req-1","observations":[{"kind":"object","label":"button","confidence":0.9,"region":{"x":1,"y":2,"width":3,"height":4}}]}`)), Header: make(http.Header)}, nil
	})
	result, err := (Client{BaseURL: "http://blockade.test", HTTPClient: &http.Client{Transport: transport}}).Observe(context.Background(), ObserveRequest{
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
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       io.NopCloser(strings.NewReader(`{"error":"Blockade provider adapter failed: rate_limit","kind":"rate_limit"}`)),
			Header:     make(http.Header),
		}, nil
	})
	_, err := (Client{BaseURL: "http://blockade.test", HTTPClient: &http.Client{Transport: transport}}).Observe(context.Background(), ObserveRequest{Image: []byte("image")})
	if kind, ok := ProviderAdapterErrorKindOf(err); !ok || kind != ProviderAdapterErrorRateLimit {
		t.Fatalf("error = %v, kind = %q", err, kind)
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
