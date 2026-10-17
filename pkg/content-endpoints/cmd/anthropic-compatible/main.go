// cymonkey-content-plugin-anthropic-compatible adapts Anthropic Messages-compatible
// endpoints to Cymonkey content interpretation.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"jangolova/contentkit"
	"net/http"
	"os"
	"strings"

	"cymonkey-content-plugin-endpoints/internal/endpoint"
	"cymonkey-content-plugin-endpoints/internal/providerexec"
)

type provider struct {
	client     *http.Client
	config     endpoint.Config
	apiVersion string
	ready      bool
}

type messageRequest struct {
	Model       string    `json:"model"`
	System      string    `json:"system"`
	Messages    []message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature *float64  `json:"temperature,omitempty"`
}

type message struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type   string       `json:"type"`
	Text   string       `json:"text,omitempty"`
	Source *imageSource `json:"source,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

func main() {
	descriptor, err := providerexec.LoadDescriptor(os.Args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := providerexec.Serve(context.Background(), descriptor, &provider{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (p *provider) Start(_ context.Context, request providerexec.StartRequest) error {
	if p.ready {
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, errors.New("provider is already started"))
	}
	config, err := endpoint.ParseConfig(request.Settings, endpoint.ParseOptions{
		DefaultBaseURL: "https://api.anthropic.com", DefaultModalities: []string{"text"},
		AllowedModalities: []string{"text", "image"}, ExtraSettings: []string{"anthropicVersion"},
	})
	if err != nil {
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, err)
	}
	version := strings.TrimSpace(request.Settings["anthropicVersion"])
	if version == "" {
		version = "2023-06-01"
	}
	if len(version) > 64 {
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, errors.New("anthropicVersion is too long"))
	}
	p.config = config
	p.apiVersion = version
	p.ready = true
	return nil
}

func (p *provider) Capabilities(context.Context) (contentkit.ProviderAdapterCapabilities, error) {
	capabilities := []string{contentkit.CapabilityTextInterpret}
	if p.ready {
		capabilities = append([]string(nil), p.config.Capabilities...)
	}
	return contentkit.ProviderAdapterCapabilities{APIVersion: contentkit.ProviderAdapterAPIVersion, Capabilities: capabilities}, nil
}

func (p *provider) Health(context.Context) (contentkit.ProviderAdapterHealth, error) {
	return contentkit.ProviderAdapterHealth{APIVersion: contentkit.ProviderAdapterAPIVersion, Ready: p.ready}, nil
}

func (p *provider) Interpret(ctx context.Context, wrapped contentkit.ProviderAdapterInterpretRequest, secrets map[string]string) (contentkit.ProviderAdapterInterpretResponse, error) {
	if !p.ready {
		return contentkit.ProviderAdapterInterpretResponse{}, contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorUnavailable, errors.New("provider has not been started"))
	}
	if wrapped.APIVersion != contentkit.ProviderAdapterAPIVersion {
		return contentkit.ProviderAdapterInterpretResponse{}, contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, errors.New("unsupported provider adapter apiVersion"))
	}
	if err := p.config.ValidateRequest(wrapped.Request); err != nil {
		return contentkit.ProviderAdapterInterpretResponse{}, err
	}
	blocks := make([]contentBlock, 0, len(wrapped.Request.Parts)+1)
	if prompt := strings.TrimSpace(wrapped.Request.Prompt); prompt != "" {
		blocks = append(blocks, contentBlock{Type: "text", Text: "Task: " + prompt})
	}
	for index, part := range wrapped.Request.Parts {
		switch part.Kind {
		case "text":
			blocks = append(blocks, contentBlock{Type: "text", Text: fmt.Sprintf("Content part %d:\n%s", index, part.Text)})
		case "image":
			blocks = append(blocks, contentBlock{Type: "text", Text: fmt.Sprintf("Image content part %d:", index)})
			blocks = append(blocks, contentBlock{Type: "image", Source: &imageSource{Type: "base64", MediaType: part.MediaType, Data: base64.StdEncoding.EncodeToString(part.Data)}})
		}
	}
	body := messageRequest{
		Model: p.config.Model, System: endpoint.ResponseInstruction,
		Messages: []message{{Role: "user", Content: blocks}}, MaxTokens: p.config.MaxTokens,
		Temperature: p.config.Temperature,
	}
	request, err := endpoint.NewJSONRequest(ctx, http.MethodPost, p.config.URL("v1/messages"), body)
	if err != nil {
		return contentkit.ProviderAdapterInterpretResponse{}, err
	}
	request.Header.Set("anthropic-version", p.apiVersion)
	if key := strings.TrimSpace(secrets["apiKey"]); key != "" {
		request.Header.Set("x-api-key", key)
	}
	var response struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := endpoint.DoJSON(ctx, p.client, request, &response); err != nil {
		return contentkit.ProviderAdapterInterpretResponse{}, err
	}
	var text strings.Builder
	for _, block := range response.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return endpoint.Normalize(text.String(), wrapped.Request)
}

func (p *provider) Close() error {
	p.ready = false
	return nil
}
