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

func TestGoogleCompatibleInterpret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1beta/models/gemini-test:generateContent" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("x-goog-api-key") != "secret" {
			t.Errorf("x-goog-api-key = %q", request.Header.Get("x-goog-api-key"))
		}
		var body generateRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Contents[0].Parts) != 2 || body.Contents[0].Parts[1].InlineData == nil {
			t.Errorf("body = %#v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"audio understood"}]}}]}`))
	}))
	defer server.Close()

	provider := &provider{client: server.Client()}
	if err := provider.Start(context.Background(), providerexec.StartRequest{Settings: map[string]string{
		"baseURL": server.URL + "/v1beta", "model": "gemini-test", "modalities": "audio",
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := provider.Interpret(context.Background(), blockade.ProviderAdapterInterpretRequest{
		APIVersion: blockade.ProviderAdapterAPIVersion,
		Request: blockade.InterpretRequest{
			APIVersion: blockade.ContentAPIVersion, RequestID: "google-1",
			Parts: []blockade.ContentPart{{Kind: "audio", MediaType: "audio/wav", Data: []byte("wav")}},
		},
	}, map[string]string{"apiKey": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Findings[0].Text != "audio understood" {
		t.Fatalf("response = %#v", result)
	}
}

func TestGoogleCompatibleEscapesModelAsOnePathSegment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.EscapedPath() != "/v1beta/models/publisher%2Fmodel:generateContent" {
			t.Errorf("escaped path = %q", request.URL.EscapedPath())
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	defer server.Close()
	provider := &provider{client: server.Client()}
	if err := provider.Start(context.Background(), providerexec.StartRequest{Settings: map[string]string{
		"baseURL": server.URL + "/v1beta", "model": "publisher/model",
	}}); err != nil {
		t.Fatal(err)
	}
	_, err := provider.Interpret(context.Background(), blockade.ProviderAdapterInterpretRequest{
		APIVersion: blockade.ProviderAdapterAPIVersion,
		Request: blockade.InterpretRequest{
			APIVersion: blockade.ContentAPIVersion,
			Parts:      []blockade.ContentPart{{Kind: "text", Text: "hello"}},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}
