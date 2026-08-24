package blockade

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestWorkerPoolObserve(t *testing.T) {
	pool, err := NewWorkerPool(context.Background(), WorkerConfig{
		Command: []string{os.Args[0], "-test.run=TestWorkerHelperProcess"},
		Env:     []string{"BLOCKADE_WORKER_HELPER=1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	result, err := (Client{WorkerPool: pool}).Observe(context.Background(), ObserveRequest{Image: []byte("image")})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Observations) != 1 || result.Observations[0].Label != "worker-object" {
		t.Fatalf("result = %#v", result)
	}
}

func TestWorkerHelperProcess(t *testing.T) {
	if os.Getenv("BLOCKADE_WORKER_HELPER") != "1" {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var request workerRequest
		if err := decoder.Decode(&request); err != nil {
			return
		}
		_ = encoder.Encode(workerResponse{ID: request.ID, OK: true, APIVersion: APIVersion, Observations: []Observation{{Kind: "object", Label: "worker-object", Confidence: 1, Region: Region{Width: 1, Height: 1}}}})
	}
}

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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestObserveRequestImageCanBeEncodedForWorker(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("image"))
	if encoded == "" || !strings.Contains(encoded, "aW1hZ2U") {
		t.Fatalf("unexpected image encoding %q", encoded)
	}
}
