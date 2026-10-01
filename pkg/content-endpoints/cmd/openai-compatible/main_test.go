package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"blockade"
	"cymonkey-content-plugin-endpoints/internal/providerexec"
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

func TestOpenAICompatibleVideoContentTypes(t *testing.T) {
	tests := []struct {
		profile  string
		typeName string
	}{
		{profile: "vllm", typeName: "video_url"},
		{profile: "llama.cpp", typeName: "input_video"},
	}
	for _, test := range tests {
		t.Run(test.profile, func(t *testing.T) {
			provider := &provider{}
			if err := provider.Start(context.Background(), providerexec.StartRequest{Settings: map[string]string{
				"profile": test.profile, "model": "video-model", "modalities": "video",
			}}); err != nil {
				t.Fatal(err)
			}
			content, err := provider.openAIContent(blockade.InterpretRequest{
				Parts: []blockade.ContentPart{{Kind: "video", MediaType: "video/mp4", Data: []byte("mp4")}},
			})
			if err != nil {
				t.Fatal(err)
			}
			parts, ok := content.([]chatContent)
			if !ok || len(parts) != 2 || parts[1].Type != test.typeName {
				t.Fatalf("content = %#v", content)
			}
			if test.typeName == "video_url" && (parts[1].VideoURL == nil || !strings.HasPrefix(parts[1].VideoURL.URL, "data:video/mp4;base64,")) {
				t.Fatalf("video_url content = %#v", parts[1])
			}
			if test.typeName == "input_video" && (parts[1].InputVideo == nil || parts[1].InputVideo.Data == "") {
				t.Fatalf("input_video content = %#v", parts[1])
			}
			capabilities, err := provider.Capabilities(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, capability := range capabilities.Capabilities {
				found = found || capability == blockade.CapabilityVideoInterpret
			}
			if !found {
				t.Fatalf("capabilities = %#v", capabilities.Capabilities)
			}
		})
	}
}

func TestOpenAICompatibleVideoContentTypeOverride(t *testing.T) {
	provider := &provider{}
	if err := provider.Start(context.Background(), providerexec.StartRequest{Settings: map[string]string{
		"profile": "custom", "model": "video-model", "modalities": "video", "videoContentType": "input_video",
	}}); err != nil {
		t.Fatal(err)
	}
	if provider.videoContentType != "input_video" {
		t.Fatalf("video content type = %q", provider.videoContentType)
	}
}
