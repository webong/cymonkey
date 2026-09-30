package blockade

import (
	"bytes"
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
	BaseURL    string
	HTTPClient *http.Client
	Engine     Engine
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

func (c Client) Observe(ctx context.Context, request ObserveRequest) (ObserveResponse, error) {
	if c.Engine != nil {
		return c.Engine.Observe(ctx, request)
	}
	hc, err := c.client()
	if err != nil {
		return ObserveResponse{}, err
	}
	request.APIVersion = APIVersion
	body, err := json.Marshal(request)
	if err != nil {
		return ObserveResponse{}, fmt.Errorf("encode Blockade request: %w", err)
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/v1/observe"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ObserveResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return ObserveResponse{}, fmt.Errorf("call Blockade: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return ObserveResponse{}, err
	}
	if resp.StatusCode/100 != 2 {
		var failure struct {
			Error string                   `json:"error"`
			Kind  ProviderAdapterErrorKind `json:"kind"`
		}
		if json.Unmarshal(data, &failure) == nil && validProviderAdapterErrorKind(failure.Kind) {
			adapterError := newProviderAdapterError("", failure.Kind, fmt.Errorf("Blockade returned HTTP %d: %s", resp.StatusCode, failure.Error))
			adapterError.RetryAfter = parseHTTPRetryAfter(resp.Header.Get("Retry-After"))
			return ObserveResponse{}, adapterError
		}
		return ObserveResponse{}, fmt.Errorf("Blockade returned HTTP %d", resp.StatusCode)
	}
	var result ObserveResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return ObserveResponse{}, fmt.Errorf("decode Blockade response: %w", err)
	}
	if result.APIVersion != APIVersion {
		return ObserveResponse{}, fmt.Errorf("unsupported Blockade apiVersion %q", result.APIVersion)
	}
	return result, nil
}

func (c Client) Interpret(ctx context.Context, request InterpretRequest) (InterpretResponse, error) {
	if request.APIVersion == "" {
		request.APIVersion = ContentAPIVersion
	}
	if err := ValidateInterpretRequest(request); err != nil {
		return InterpretResponse{}, err
	}
	if c.Engine != nil {
		engine, ok := c.Engine.(ContentEngine)
		if !ok {
			return InterpretResponse{}, ErrModalityUnsupported
		}
		result, err := engine.Interpret(ctx, request)
		if err != nil {
			return InterpretResponse{}, err
		}
		if err := ValidateInterpretResponse(result, request); err != nil {
			return InterpretResponse{}, err
		}
		return result, nil
	}
	hc, err := c.client()
	if err != nil {
		return InterpretResponse{}, err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return InterpretResponse{}, fmt.Errorf("encode Blockade content request: %w", err)
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/v1/interpret"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return InterpretResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return InterpretResponse{}, fmt.Errorf("call Blockade: %w", err)
	}
	defer resp.Body.Close()
	responseData, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return InterpretResponse{}, err
	}
	if resp.StatusCode/100 != 2 {
		if resp.StatusCode == http.StatusNotImplemented {
			return InterpretResponse{}, ErrModalityUnsupported
		}
		var failure struct {
			Error string                   `json:"error"`
			Kind  ProviderAdapterErrorKind `json:"kind"`
		}
		if json.Unmarshal(responseData, &failure) == nil && validProviderAdapterErrorKind(failure.Kind) {
			adapterError := newProviderAdapterError("", failure.Kind, fmt.Errorf("Blockade returned HTTP %d: %s", resp.StatusCode, failure.Error))
			adapterError.RetryAfter = parseHTTPRetryAfter(resp.Header.Get("Retry-After"))
			return InterpretResponse{}, adapterError
		}
		return InterpretResponse{}, fmt.Errorf("Blockade returned HTTP %d", resp.StatusCode)
	}
	var result InterpretResponse
	if err := json.Unmarshal(responseData, &result); err != nil {
		return InterpretResponse{}, fmt.Errorf("decode Blockade content response: %w", err)
	}
	if err := ValidateInterpretResponse(result, request); err != nil {
		return InterpretResponse{}, err
	}
	return result, nil
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
