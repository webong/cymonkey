// cymonkey-content-plugin-openai-compatible adapts OpenAI Chat Completions and
// compatible servers to Cymonkey content interpretation.
package main

import (
	"contentkit"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"cymonkey-content-plugin-endpoints/internal/endpoint"
	"cymonkey-content-plugin-endpoints/internal/providerexec"
)

var profileBaseURLs = map[string]string{
	"openai":    "https://api.openai.com/v1",
	"vllm":      "http://127.0.0.1:8000/v1",
	"sglang":    "http://127.0.0.1:30000/v1",
	"ollama":    "http://127.0.0.1:11434/v1",
	"llama.cpp": "http://127.0.0.1:8080/v1",
	"lm-studio": "http://127.0.0.1:1234/v1",
	"custom":    "http://127.0.0.1:8000/v1",
}

type provider struct {
	client           *http.Client
	config           endpoint.Config
	ready            bool
	format           string
	videoContentType string
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	MaxTokens      int            `json:"max_tokens"`
	Temperature    *float64       `json:"temperature,omitempty"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type chatContent struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ImageURL   *chatImageURL   `json:"image_url,omitempty"`
	InputAudio *chatInputAudio `json:"input_audio,omitempty"`
	VideoURL   *chatVideoURL   `json:"video_url,omitempty"`
	InputVideo *chatInputVideo `json:"input_video,omitempty"`
}

type chatImageURL struct {
	URL string `json:"url"`
}

type chatInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type chatVideoURL struct {
	URL string `json:"url"`
}

type chatInputVideo struct {
	Data string `json:"data"`
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
	profile := strings.ToLower(strings.TrimSpace(request.Settings["profile"]))
	if profile == "" {
		profile = "openai"
	}
	defaultBaseURL, ok := profileBaseURLs[profile]
	if !ok {
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, fmt.Errorf("unknown OpenAI-compatible profile %q", profile))
	}
	config, err := endpoint.ParseConfig(request.Settings, endpoint.ParseOptions{
		DefaultBaseURL: defaultBaseURL, DefaultModalities: []string{"text"},
		AllowedModalities: []string{"text", "image", "audio", "video"},
		ExtraSettings:     []string{"profile", "responseFormat", "videoContentType"},
	})
	if err != nil {
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, err)
	}
	format := strings.ToLower(strings.TrimSpace(request.Settings["responseFormat"]))
	if format == "" {
		format = "none"
	}
	if format != "none" && format != "json_object" {
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, errors.New("responseFormat must be none or json_object"))
	}
	videoContentType := strings.ToLower(strings.TrimSpace(request.Settings["videoContentType"]))
	if videoContentType == "" {
		videoContentType = "video_url"
		if profile == "llama.cpp" {
			videoContentType = "input_video"
		}
	}
	if videoContentType != "video_url" && videoContentType != "input_video" {
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, errors.New("videoContentType must be video_url or input_video"))
	}
	p.config = config
	p.format = format
	p.videoContentType = videoContentType
	p.ready = true
	return nil
}

func (p *provider) Capabilities(context.Context) (contentkit.ProviderAdapterCapabilities, error) {
	if !p.ready {
		return contentkit.ProviderAdapterCapabilities{APIVersion: contentkit.ProviderAdapterAPIVersion, Capabilities: []string{contentkit.CapabilityTextInterpret}}, nil
	}
	return contentkit.ProviderAdapterCapabilities{APIVersion: contentkit.ProviderAdapterAPIVersion, Capabilities: append([]string(nil), p.config.Capabilities...)}, nil
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
	content, err := p.openAIContent(wrapped.Request)
	if err != nil {
		return contentkit.ProviderAdapterInterpretResponse{}, err
	}
	body := chatRequest{
		Model: p.config.Model, MaxTokens: p.config.MaxTokens, Temperature: p.config.Temperature,
		Messages: []chatMessage{
			{Role: "system", Content: endpoint.ResponseInstruction},
			{Role: "user", Content: content},
		},
	}
	if p.format == "json_object" {
		body.ResponseFormat = map[string]any{"type": "json_object"}
	}
	request, err := endpoint.NewJSONRequest(ctx, http.MethodPost, p.config.URL("chat/completions"), body)
	if err != nil {
		return contentkit.ProviderAdapterInterpretResponse{}, err
	}
	if key := strings.TrimSpace(secrets["apiKey"]); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := endpoint.DoJSON(ctx, p.client, request, &response); err != nil {
		return contentkit.ProviderAdapterInterpretResponse{}, err
	}
	if len(response.Choices) == 0 {
		return contentkit.ProviderAdapterInterpretResponse{}, contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidResponse, errors.New("endpoint returned no choices"))
	}
	text, err := openAIResponseText(response.Choices[0].Message.Content)
	if err != nil {
		return contentkit.ProviderAdapterInterpretResponse{}, err
	}
	return endpoint.Normalize(text, wrapped.Request)
}

func (p *provider) Close() error {
	p.ready = false
	return nil
}

func (p *provider) openAIContent(request contentkit.InterpretRequest) (any, error) {
	content := make([]chatContent, 0, len(request.Parts)+1)
	if prompt := strings.TrimSpace(request.Prompt); prompt != "" {
		content = append(content, chatContent{Type: "text", Text: "Task: " + prompt})
	}
	for index, part := range request.Parts {
		switch part.Kind {
		case "text":
			content = append(content, chatContent{Type: "text", Text: fmt.Sprintf("Content part %d:\n%s", index, part.Text)})
		case "image":
			content = append(content, chatContent{Type: "text", Text: fmt.Sprintf("Image content part %d:", index)})
			content = append(content, chatContent{Type: "image_url", ImageURL: &chatImageURL{URL: "data:" + part.MediaType + ";base64," + base64.StdEncoding.EncodeToString(part.Data)}})
		case "audio":
			format, err := openAIAudioFormat(part.MediaType)
			if err != nil {
				return nil, err
			}
			content = append(content, chatContent{Type: "text", Text: fmt.Sprintf("Audio content part %d:", index)})
			content = append(content, chatContent{Type: "input_audio", InputAudio: &chatInputAudio{Data: base64.StdEncoding.EncodeToString(part.Data), Format: format}})
		case "video":
			content = append(content, chatContent{Type: "text", Text: fmt.Sprintf("Video content part %d:", index)})
			encoded := base64.StdEncoding.EncodeToString(part.Data)
			if p.videoContentType == "input_video" {
				content = append(content, chatContent{Type: "input_video", InputVideo: &chatInputVideo{Data: encoded}})
			} else {
				content = append(content, chatContent{Type: "video_url", VideoURL: &chatVideoURL{URL: "data:" + part.MediaType + ";base64," + encoded}})
			}
		}
	}
	if len(content) == 1 && content[0].Type == "text" {
		return content[0].Text, nil
	}
	return content, nil
}

func openAIAudioFormat(mediaType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0])) {
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "wav", nil
	case "audio/mpeg", "audio/mp3":
		return "mp3", nil
	default:
		return "", contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, fmt.Errorf("OpenAI-compatible audio input does not support media type %q", mediaType))
	}
}

func openAIResponseText(raw json.RawMessage) (string, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidResponse, errors.New("endpoint returned unsupported message content"))
	}
	var builder strings.Builder
	for _, part := range parts {
		if part.Type == "text" || part.Type == "output_text" {
			builder.WriteString(part.Text)
		}
	}
	return builder.String(), nil
}
