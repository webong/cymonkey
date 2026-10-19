// Package generation supplies opt-in HTTP model clients for Blockade's generation
// and speech contracts. Hosts choose endpoints, models, and credential sources.
package generation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"jangolova/contentkit"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"jangolova/generationkit"
)

type Protocol string

const (
	OpenAI    Protocol = "openai-compatible"
	Responses Protocol = "responses-compatible"
	Anthropic Protocol = "anthropic"
	Gemini    Protocol = "gemini"
	Cohere    Protocol = "cohere-embeddings"
	Voyage    Protocol = "voyage-embeddings"
)

type Config struct {
	Protocol           Protocol
	BaseURL            string // API root, including /v1 or /v1beta
	HTTPClient         *http.Client
	APIKey             func(context.Context) (string, error)
	MaxResponseBytes   int64
	EmbeddingInputType string // required by Cohere; explicit, model-dependent
}
type Client struct{ config Config }

func New(config Config) (*Client, error) {
	u, err := url.Parse(config.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("model endpoint needs an HTTP(S) API root without credentials, query, or fragment")
	}
	switch config.Protocol {
	case OpenAI, Responses, Anthropic, Gemini, Cohere, Voyage:
	default:
		return nil, errors.New("unsupported model endpoint protocol")
	}
	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = 32 << 20
	}
	if config.MaxResponseBytes < 1 || config.MaxResponseBytes > 256<<20 {
		return nil, errors.New("model response limit must be between 1 byte and 256 MiB")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")
	return &Client{config}, nil
}

// EnvironmentKey resolves only the explicitly named environment variable.
func EnvironmentKey(name string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		key := os.Getenv(name)
		if key == "" {
			return "", fmt.Errorf("API key environment variable %q is not set", name)
		}
		return key, nil
	}
}

type Error struct {
	Status     int
	Kind       string
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("model endpoint returned HTTP %d (%s)", e.Status, e.Kind)
}

func (e *Error) Unwrap() error {
	kind := contentkit.ErrorUnavailable
	switch e.Kind {
	case "authentication":
		kind = contentkit.ErrorAuthentication
	case "rate_limit":
		kind = contentkit.ErrorRateLimit
	case "invalid_request", "not_found":
		kind = contentkit.ErrorInvalidInput
	}
	return &contentkit.ModelError{Kind: kind, Operation: "http", Retryable: e.Status == 429 || e.Status >= 500, RetryAfter: e.RetryAfter}
}

func (c *Client) do(ctx context.Context, method, path, contentType string, body io.Reader, stream bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.config.BaseURL+path, body)
	if err != nil {
		return nil, &contentkit.ModelError{Kind: contentkit.ClassifyModelError(err), Operation: "http", Cause: err}
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if c.config.APIKey != nil {
		key, err := c.config.APIKey(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve model credentials: %w", err)
		}
		switch c.config.Protocol {
		case Anthropic:
			req.Header.Set("x-api-key", key)
		case Gemini:
			req.Header.Set("x-goog-api-key", key)
		default:
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	if c.config.Protocol == Anthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := c.config.HTTPClient.Do(req)
	if err != nil {
		return nil, &contentkit.ModelError{Kind: contentkit.ErrorTransport, Operation: "request", Cause: err}
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		kind := "provider"
		switch resp.StatusCode {
		case 401, 403:
			kind = "authentication"
		case 429:
			kind = "rate_limit"
		case 400, 422:
			kind = "invalid_request"
		case 404:
			kind = "not_found"
		default:
			if resp.StatusCode >= 500 {
				kind = "unavailable"
			}
		}
		retry := time.Duration(0)
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 && seconds < 31536000 {
			retry = time.Duration(seconds) * time.Second
		}
		return nil, &Error{resp.StatusCode, kind, retry}
	}
	return resp, nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("model response exceeds configured byte limit")
	}
	return data, nil
}

func (c *Client) Generate(ctx context.Context, request generationkit.GenerationRequest) (generationkit.GenerationResponse, error) {
	body, path, err := c.encode(request, false)
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, path, "application/json", bytes.NewReader(body), false)
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	defer resp.Body.Close()
	data, err := readBounded(resp.Body, c.config.MaxResponseBytes)
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	result, err := c.decode(data, request.Model)
	if err != nil {
		if errors.Is(err, contentkit.ErrGenerationUnsupported) {
			return generationkit.GenerationResponse{}, err
		}
		return generationkit.GenerationResponse{}, &contentkit.ModelError{Kind: contentkit.ErrorInvalidResponse, Operation: "generate", Cause: err}
	}
	if err := generationkit.ValidateGenerationResult(result, request); err != nil {
		return generationkit.GenerationResponse{}, &contentkit.ModelError{Kind: contentkit.ErrorInvalidResponse, Operation: "generate", Cause: err}
	}
	return result, nil
}
