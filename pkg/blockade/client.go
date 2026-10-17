package blockade

import (
	"bytes"
	"contentkit"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL     string
	HTTPClient  *http.Client
	Engine      contentkit.Engine
	BearerToken string
}

type ServiceHealth struct {
	Status   string `json:"status"`
	Provider string `json:"provider"`
}

const maximumHTTPRetryAfter = 365 * 24 * time.Hour

func (c Client) client() (*http.Client, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, errors.New("Blockade endpoint is required")
	}
	if c.HTTPClient != nil {
		return c.HTTPClient, nil
	}
	return &http.Client{Timeout: 2 * time.Minute}, nil
}

func (c Client) Observe(ctx context.Context, request contentkit.ObserveRequest) (contentkit.ObserveResponse, error) {
	if c.Engine != nil {
		return c.Engine.Observe(ctx, request)
	}
	hc, err := c.client()
	if err != nil {
		return contentkit.ObserveResponse{}, err
	}
	request.APIVersion = contentkit.APIVersion
	body, err := json.Marshal(request)
	if err != nil {
		return contentkit.ObserveResponse{}, fmt.Errorf("encode Blockade request: %w", err)
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/v1/observe"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return contentkit.ObserveResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)
	resp, err := hc.Do(req)
	if err != nil {
		return contentkit.ObserveResponse{}, fmt.Errorf("call Blockade: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return contentkit.ObserveResponse{}, err
	}
	if resp.StatusCode/100 != 2 {
		var failure struct {
			Error string                              `json:"error"`
			Kind  contentkit.ProviderAdapterErrorKind `json:"kind"`
		}
		if json.Unmarshal(data, &failure) == nil && contentkit.ValidProviderAdapterErrorKind(failure.Kind) {
			adapterError := contentkit.NewProviderAdapterError(failure.Kind, fmt.Errorf("Blockade returned HTTP %d: %s", resp.StatusCode, failure.Error))
			adapterError.RetryAfter = parseHTTPRetryAfter(resp.Header.Get("Retry-After"))
			return contentkit.ObserveResponse{}, adapterError
		}
		return contentkit.ObserveResponse{}, fmt.Errorf("Blockade returned HTTP %d", resp.StatusCode)
	}
	var result contentkit.ObserveResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return contentkit.ObserveResponse{}, fmt.Errorf("decode Blockade response: %w", err)
	}
	if result.APIVersion != contentkit.APIVersion {
		return contentkit.ObserveResponse{}, fmt.Errorf("unsupported Blockade apiVersion %q", result.APIVersion)
	}
	return result, nil
}

func (c Client) Interpret(ctx context.Context, request contentkit.InterpretRequest) (contentkit.InterpretResponse, error) {
	if request.APIVersion == "" {
		request.APIVersion = contentkit.ContentAPIVersion
	}
	if err := contentkit.ValidateInterpretRequest(request); err != nil {
		return contentkit.InterpretResponse{}, err
	}
	if c.Engine != nil {
		engine, ok := c.Engine.(contentkit.ContentEngine)
		if !ok {
			return contentkit.InterpretResponse{}, contentkit.ErrModalityUnsupported
		}
		result, err := engine.Interpret(ctx, request)
		if err != nil {
			return contentkit.InterpretResponse{}, err
		}
		if err := contentkit.ValidateInterpretResponse(result, request); err != nil {
			return contentkit.InterpretResponse{}, err
		}
		return result, nil
	}
	hc, err := c.client()
	if err != nil {
		return contentkit.InterpretResponse{}, err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return contentkit.InterpretResponse{}, fmt.Errorf("encode Blockade content request: %w", err)
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/v1/interpret"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return contentkit.InterpretResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)
	resp, err := hc.Do(req)
	if err != nil {
		return contentkit.InterpretResponse{}, fmt.Errorf("call Blockade: %w", err)
	}
	defer resp.Body.Close()
	responseData, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return contentkit.InterpretResponse{}, err
	}
	if resp.StatusCode/100 != 2 {
		if resp.StatusCode == http.StatusNotImplemented {
			return contentkit.InterpretResponse{}, contentkit.ErrModalityUnsupported
		}
		var failure struct {
			Error string                              `json:"error"`
			Kind  contentkit.ProviderAdapterErrorKind `json:"kind"`
		}
		if json.Unmarshal(responseData, &failure) == nil && contentkit.ValidProviderAdapterErrorKind(failure.Kind) {
			adapterError := contentkit.NewProviderAdapterError(failure.Kind, fmt.Errorf("Blockade returned HTTP %d: %s", resp.StatusCode, failure.Error))
			adapterError.RetryAfter = parseHTTPRetryAfter(resp.Header.Get("Retry-After"))
			return contentkit.InterpretResponse{}, adapterError
		}
		return contentkit.InterpretResponse{}, fmt.Errorf("Blockade returned HTTP %d", resp.StatusCode)
	}
	var result contentkit.InterpretResponse
	if err := json.Unmarshal(responseData, &result); err != nil {
		return contentkit.InterpretResponse{}, fmt.Errorf("decode Blockade content response: %w", err)
	}
	if err := contentkit.ValidateInterpretResponse(result, request); err != nil {
		return contentkit.InterpretResponse{}, err
	}
	return result, nil
}

func (c Client) Health(ctx context.Context) (ServiceHealth, error) {
	if c.Engine != nil {
		result := ServiceHealth{Status: "ok", Provider: "embedded"}
		if reporter, ok := c.Engine.(contentkit.EngineHealthReporter); ok {
			health, err := reporter.Health(ctx)
			if err != nil {
				return ServiceHealth{}, err
			}
			if !health.Ready {
				result.Status = "not_ready"
			}
		}
		return result, nil
	}
	var result ServiceHealth
	status, err := c.getJSON(ctx, "/healthz", &result)
	if err != nil {
		return ServiceHealth{}, err
	}
	if status != http.StatusOK {
		return result, fmt.Errorf("Blockade health returned HTTP %d", status)
	}
	if result.Status != "ok" {
		return result, fmt.Errorf("Blockade service is %s", result.Status)
	}
	return result, nil
}

func (c Client) Capabilities(ctx context.Context) (contentkit.CapabilitiesResponse, error) {
	if c.Engine != nil {
		capabilities := []string{contentkit.CapabilityImageObserve}
		if reporter, ok := c.Engine.(contentkit.EngineCapabilities); ok {
			var err error
			capabilities, err = reporter.Capabilities(ctx)
			if err != nil {
				return contentkit.CapabilitiesResponse{}, err
			}
		}
		return contentkit.CapabilitiesResponse{APIVersion: contentkit.APIVersion, Provider: "embedded", Capabilities: capabilities}, nil
	}
	var result contentkit.CapabilitiesResponse
	status, err := c.getJSON(ctx, "/capabilities", &result)
	if err != nil {
		return contentkit.CapabilitiesResponse{}, err
	}
	if status/100 != 2 {
		return contentkit.CapabilitiesResponse{}, fmt.Errorf("Blockade capabilities returned HTTP %d", status)
	}
	if result.APIVersion != contentkit.APIVersion {
		return contentkit.CapabilitiesResponse{}, fmt.Errorf("unsupported Blockade apiVersion %q", result.APIVersion)
	}
	return result, nil
}

func (c Client) getJSON(ctx context.Context, path string, target any) (int, error) {
	httpClient, err := c.client()
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+path, nil)
	if err != nil {
		return 0, err
	}
	c.authorize(request)
	response, err := httpClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("call Blockade: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return response.StatusCode, err
	}
	if len(data) > 1<<20 {
		return response.StatusCode, errors.New("Blockade response exceeds 1 MiB")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return response.StatusCode, fmt.Errorf("decode Blockade response: %w", err)
	}
	return response.StatusCode, nil
}

func (c Client) authorize(request *http.Request) {
	if token := strings.TrimSpace(c.BearerToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}

func parseHTTPRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 && seconds <= int64(maximumHTTPRetryAfter/time.Second) {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delay := time.Until(when)
	if delay < 0 || delay > maximumHTTPRetryAfter {
		return 0
	}
	return delay
}
