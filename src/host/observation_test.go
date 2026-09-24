package cymonkeyhost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cymonkey/lib/blockade"
)

func TestObservationCoordinatorCoordinatesJangolovaAndBlockade(t *testing.T) {
	jangolova := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/instances/browser-1/call" || r.Header.Get("Authorization") != "Bearer test-token" {
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
			t.Fatalf("Blockade path = %q", r.URL.Path)
		}
		var request struct {
			Image  string `json:"image"`
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Image != "cG5n" || request.Prompt != "find button" {
			t.Fatalf("Blockade request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(blockade.ObserveResponse{APIVersion: blockade.APIVersion, RequestID: "observation-1"})
	}))
	defer blockadeServer.Close()

	t.Setenv("CYMONKEY_TEST_TOKEN", "test-token")
	coordinator, err := NewObservationCoordinator(ObservationConfig{
		Jangolova: ObservationJangolovaConfig{Endpoint: jangolova.URL, TokenEnvironment: "CYMONKEY_TEST_TOKEN"},
		Blockade:  ObservationBlockadeConfig{Endpoint: blockadeServer.URL},
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
