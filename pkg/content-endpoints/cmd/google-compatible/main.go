// cymonkey-content-plugin-google-compatible adapts Gemini generateContent-compatible
// endpoints to Cymonkey content interpretation.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"jangolova/contentkit"
	"net/http"
	"net/url"
	"os"
	"strings"

	"cymonkey-content-plugin-endpoints/internal/endpoint"
	"cymonkey-content-plugin-endpoints/internal/providerexec"
)

type provider struct {
	client *http.Client
	config endpoint.Config
	ready  bool
}

type generateRequest struct {
	SystemInstruction content          `json:"systemInstruction"`
	Contents          []content        `json:"contents"`
	GenerationConfig  generationConfig `json:"generationConfig"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inlineData,omitempty"`
}

type inlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type generationConfig struct {
	MaxOutputTokens int      `json:"maxOutputTokens"`
	Temperature     *float64 `json:"temperature,omitempty"`
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
		DefaultBaseURL:    "https://generativelanguage.googleapis.com/v1beta",
		DefaultModalities: []string{"text"}, AllowedModalities: []string{"text", "image", "audio", "video"},
	})
	if err != nil {
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, err)
	}
	p.config = config
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
	parts := make([]part, 0, len(wrapped.Request.Parts)+1)
	if prompt := strings.TrimSpace(wrapped.Request.Prompt); prompt != "" {
		parts = append(parts, part{Text: "Task: " + prompt})
	}
	for index, item := range wrapped.Request.Parts {
		switch item.Kind {
		case "text":
			parts = append(parts, part{Text: fmt.Sprintf("Content part %d:\n%s", index, item.Text)})
		case "image", "audio", "video":
			parts = append(parts, part{Text: fmt.Sprintf("%s content part %d:", title(item.Kind), index)})
			parts = append(parts, part{InlineData: &inlineData{MIMEType: item.MediaType, Data: base64.StdEncoding.EncodeToString(item.Data)}})
		}
	}
	body := generateRequest{
		SystemInstruction: content{Parts: []part{{Text: endpoint.ResponseInstruction}}},
		Contents:          []content{{Role: "user", Parts: parts}},
		GenerationConfig:  generationConfig{MaxOutputTokens: p.config.MaxTokens, Temperature: p.config.Temperature},
	}
	target := strings.TrimRight(p.config.BaseURL.String(), "/") + "/models/" + url.PathEscape(p.config.Model) + ":generateContent"
	request, err := endpoint.NewJSONRequest(ctx, http.MethodPost, target, body)
	if err != nil {
		return contentkit.ProviderAdapterInterpretResponse{}, err
	}
	if key := strings.TrimSpace(secrets["apiKey"]); key != "" {
		request.Header.Set("x-goog-api-key", key)
	}
	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := endpoint.DoJSON(ctx, p.client, request, &response); err != nil {
		return contentkit.ProviderAdapterInterpretResponse{}, err
	}
	if len(response.Candidates) == 0 {
		return contentkit.ProviderAdapterInterpretResponse{}, contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidResponse, errors.New("endpoint returned no candidates"))
	}
	var text strings.Builder
	for _, part := range response.Candidates[0].Content.Parts {
		text.WriteString(part.Text)
	}
	return endpoint.Normalize(text.String(), wrapped.Request)
}

func (p *provider) Close() error {
	p.ready = false
	return nil
}

func title(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
