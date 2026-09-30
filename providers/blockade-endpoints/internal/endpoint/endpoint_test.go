package endpoint

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"blockade"
)

func TestParseConfigDerivesCapabilities(t *testing.T) {
	config, err := ParseConfig(map[string]string{
		"model": "vision-model", "modalities": "text,image,video", "maxTokens": "2048",
	}, ParseOptions{
		DefaultBaseURL: "http://127.0.0.1:8000/v1", DefaultModalities: []string{"text"},
		AllowedModalities: []string{"text", "image", "video"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.Model != "vision-model" || config.MaxTokens != 2048 {
		t.Fatalf("config = %#v", config)
	}
	for _, capability := range []string{blockade.CapabilityTextInterpret, blockade.CapabilityImageInterpret, blockade.CapabilityVideoInterpret, blockade.CapabilityMultimodalInterpret} {
		if !slices.Contains(config.Capabilities, capability) {
			t.Fatalf("capabilities = %#v", config.Capabilities)
		}
	}
}

func TestParseConfigRejectsNonFiniteTemperature(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		_, err := ParseConfig(map[string]string{"model": "model", "temperature": value}, ParseOptions{
			DefaultBaseURL: "http://127.0.0.1:8000/v1", DefaultModalities: []string{"text"}, AllowedModalities: []string{"text"},
		})
		if err == nil {
			t.Fatalf("temperature %q was accepted", value)
		}
	}
}

func TestNormalizeAcceptsStructuredAndPlainModelOutput(t *testing.T) {
	request := blockade.InterpretRequest{
		APIVersion: blockade.ContentAPIVersion, RequestID: "request-1",
		Parts: []blockade.ContentPart{{Kind: "text", Text: "hello"}},
	}
	structured, err := Normalize(`{"findings":[{"kind":"summary","text":"hello back","sourceParts":[0]}]}`, request)
	if err != nil {
		t.Fatal(err)
	}
	if structured.Response.Findings[0].Kind != "summary" {
		t.Fatalf("structured response = %#v", structured)
	}
	plain, err := Normalize("plain response", request)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Response.Findings[0].Kind != "response" || plain.Response.Findings[0].Text != "plain response" {
		t.Fatalf("plain response = %#v", plain)
	}
}

func TestDoJSONMapsRateLimitAndRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", "2")
		http.Error(writer, "busy", http.StatusTooManyRequests)
	}))
	defer server.Close()
	request, err := NewJSONRequest(context.Background(), http.MethodPost, server.URL, map[string]string{"input": "value"})
	if err != nil {
		t.Fatal(err)
	}
	err = DoJSON(context.Background(), server.Client(), request, &map[string]any{})
	var adapterError *blockade.ProviderAdapterError
	if !errors.As(err, &adapterError) {
		t.Fatalf("error = %T %v", err, err)
	}
	if adapterError.Kind != blockade.ProviderAdapterErrorRateLimit || adapterError.RetryAfter != 2*time.Second {
		t.Fatalf("adapter error = %#v", adapterError)
	}
}
