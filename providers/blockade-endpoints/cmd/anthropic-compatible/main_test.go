package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"blockade"
	"blockade-provider-endpoints/internal/providerexec"
)

func TestAnthropicCompatibleInterpret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/messages" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("x-api-key") != "secret" || request.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("headers = %#v", request.Header)
		}
		var body messageRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "claude-compatible" || len(body.Messages[0].Content) != 2 || body.Messages[0].Content[1].Source == nil {
			t.Errorf("body = %#v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"content":[{"type":"text","text":"image understood"}]}`))
	}))
	defer server.Close()

	provider := &provider{client: server.Client()}
	if err := provider.Start(context.Background(), providerexec.StartRequest{Settings: map[string]string{
		"baseURL": server.URL, "model": "claude-compatible", "modalities": "image",
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := provider.Interpret(context.Background(), blockade.ProviderAdapterInterpretRequest{
		APIVersion: blockade.ProviderAdapterAPIVersion,
		Request: blockade.InterpretRequest{
			APIVersion: blockade.ContentAPIVersion, RequestID: "anthropic-1",
			Parts: []blockade.ContentPart{{Kind: "image", MediaType: "image/jpeg", Data: []byte("jpeg")}},
		},
	}, map[string]string{"apiKey": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Findings[0].Text != "image understood" {
		t.Fatalf("response = %#v", result)
	}
}
