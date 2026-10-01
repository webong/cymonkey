package contentbridge

import (
	"context"
	"cymonkey/src/contracts"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestServiceClientUsesCymonkeyContracts(t *testing.T) {
	client := ServiceClient{BaseURL: "https://content.example", HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["apiVersion"] != contracts.ContentAPIVersion || strings.Contains(string(data), "blockade") {
			t.Fatalf("service request = %s", data)
		}
		body := `{"apiVersion":"cymonkey.content/v1alpha1","findings":[{"kind":"summary","text":"done","sourceParts":[0]}]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	result, err := client.Interpret(context.Background(), contracts.ContentRequest{
		APIVersion: contracts.ContentAPIVersion,
		Parts:      []contracts.ContentPart{{Kind: "text", Text: "hello"}},
	})
	if err != nil || len(result.Findings) != 1 || result.Findings[0].Text != "done" {
		t.Fatalf("content result = %#v, %v", result, err)
	}
}
