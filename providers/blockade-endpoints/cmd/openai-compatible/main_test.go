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

func TestOpenAICompatibleInterpret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		var body chatRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "vision-model" || len(body.Messages) != 2 {
			t.Errorf("body = %#v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"{\"findings\":[{\"kind\":\"caption\",\"text\":\"a diagram\",\"sourceParts\":[0]}]}"}}]}`))
	}))
	defer server.Close()

	provider := &provider{client: server.Client()}
	if err := provider.Start(context.Background(), providerexec.StartRequest{Settings: map[string]string{
		"profile": "ollama", "baseURL": server.URL + "/v1", "model": "vision-model", "modalities": "image",
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := provider.Interpret(context.Background(), blockade.ProviderAdapterInterpretRequest{
		APIVersion: blockade.ProviderAdapterAPIVersion,
		Request: blockade.InterpretRequest{
			APIVersion: blockade.ContentAPIVersion, RequestID: "openai-1",
			Parts: []blockade.ContentPart{{Kind: "image", MediaType: "image/png", Data: []byte("png")}},
		},
	}, map[string]string{"apiKey": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Response.Findings[0]; got.Kind != "caption" || got.Text != "a diagram" {
		t.Fatalf("finding = %#v", got)
	}
}

func TestOpenAIProfilesUseExpectedLocalPorts(t *testing.T) {
	want := map[string]string{
		"vllm": "http://127.0.0.1:8000/v1", "sglang": "http://127.0.0.1:30000/v1",
		"ollama": "http://127.0.0.1:11434/v1", "llama.cpp": "http://127.0.0.1:8080/v1",
		"lm-studio": "http://127.0.0.1:1234/v1",
	}
	for profile, baseURL := range want {
		t.Run(profile, func(t *testing.T) {
			provider := &provider{}
			if err := provider.Start(context.Background(), providerexec.StartRequest{Settings: map[string]string{"profile": profile, "model": "model"}}); err != nil {
				t.Fatal(err)
			}
			if got := provider.config.BaseURL.String(); got != baseURL {
				t.Fatalf("baseURL = %q, want %q", got, baseURL)
			}
		})
	}
}
