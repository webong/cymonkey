package blockade

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type serverEngineFixture struct{}

func (serverEngineFixture) Observe(_ context.Context, request ObserveRequest) (ObserveResponse, error) {
	return ObserveResponse{
		APIVersion: APIVersion,
		RequestID:  request.RequestID,
		Observations: []Observation{{
			Kind: "object", Label: "fixture", Confidence: 0.9,
			Region: Region{X: 1, Y: 2, Width: 3, Height: 4},
		}},
	}, nil
}

func (serverEngineFixture) Close() error { return nil }

func TestHTTPServerRoutesObservation(t *testing.T) {
	server, err := NewHTTPServer(serverEngineFixture{}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	image := base64.StdEncoding.EncodeToString([]byte("png"))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/observe", strings.NewReader(`{"apiVersion":"blockade.observation/v1alpha1","requestId":"request-1","image":"`+image+`"}`))
	server.Routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response ObserveResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if err := ValidateObserveResponse(response); err != nil {
		t.Fatal(err)
	}
	if response.RequestID != "request-1" || response.Observations[0].Label != "fixture" {
		t.Fatalf("response = %#v", response)
	}
}

func TestHTTPServerRejectsMissingImage(t *testing.T) {
	server, err := NewHTTPServer(serverEngineFixture{}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/observe", strings.NewReader(`{"requestId":"request-1"}`))
	server.Routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
