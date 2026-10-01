package observation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cymonkey/src/contracts"
)

func TestObservationCoordinatorCoordinatesDisplayAndContent(t *testing.T) {
	jangolova := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/display-instances/browser-1/call" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Jangolova request = %s %s %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		var request struct {
			Method string `json:"method"`
			Params struct {
				Name  string `json:"name"`
				Input struct {
					FullPage bool `json:"fullPage"`
				} `json:"input"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Method != "act" || request.Params.Name != "window.screenshot" || !request.Params.Input.FullPage {
			t.Fatalf("capture request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]string{"pngBase64": "cG5n"}})
	}))
	defer jangolova.Close()
	blockadeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/observe" {
			t.Fatalf("content path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer blockade-token" {
			t.Fatalf("content authorization = %q", r.Header.Get("Authorization"))
		}
		var request struct {
			Image  string `json:"image"`
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Image != "cG5n" || request.Prompt != "find button" {
			t.Fatalf("content request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(contracts.ObservationResult{APIVersion: contracts.ObservationAPIVersion, RequestID: "observation-1", Observations: []contracts.Observation{}})
	}))
	defer blockadeServer.Close()

	t.Setenv("CYMONKEY_TEST_TOKEN", "test-token")
	t.Setenv("BLOCKADE_TEST_TOKEN", "blockade-token")
	coordinator, err := NewObservationCoordinator(Config{
		Capture:        ServiceConfig{Endpoint: jangolova.URL, TokenEnvironment: "CYMONKEY_TEST_TOKEN"},
		Interpretation: ServiceConfig{Endpoint: blockadeServer.URL, TokenEnvironment: "BLOCKADE_TEST_TOKEN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Observe(context.Background(), ObservationRequest{InstanceID: "browser-1", Prompt: "find button", FullPage: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.APIVersion != ObservationAPIVersion || result.CaptureAction != "window.screenshot" || result.Observation.RequestID != "observation-1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestObservationCoordinatorAcceptsHostCapture(t *testing.T) {
	blockadeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Image string `json:"image"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Image != "cG5n" {
			t.Fatalf("content request = %#v, %v", request, err)
		}
		_ = json.NewEncoder(w).Encode(contracts.ObservationResult{APIVersion: contracts.ObservationAPIVersion, RequestID: "host-capture", Observations: []contracts.Observation{}})
	}))
	defer blockadeServer.Close()
	coordinator, err := NewObservationCoordinatorWithCapture(Config{
		Interpretation: ServiceConfig{Endpoint: blockadeServer.URL},
	}, func(_ context.Context, request ObservationRequest) ([]byte, error) {
		if request.InstanceID != "browser-one" {
			t.Fatalf("capture request = %#v", request)
		}
		return []byte("png"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Observe(context.Background(), ObservationRequest{InstanceID: "browser-one"})
	if err != nil || result.Observation.RequestID != "host-capture" {
		t.Fatalf("observation = %#v, %v", result, err)
	}
}
