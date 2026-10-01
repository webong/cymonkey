package endpoint

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

	"blockade"
)

const maximumResponseBytes = 8 << 20
const maximumRetryAfter = 365 * 24 * time.Hour

func NewJSONRequest(ctx context.Context, method, target string, body any) (*http.Request, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidRequest, err)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(encoded))
	if err != nil {
		return nil, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidRequest, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	return request, nil
}

func DoJSON(ctx context.Context, client *http.Client, request *http.Request, destination any) error {
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
			return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorTimeout, err)
		case errors.Is(ctx.Err(), context.Canceled), errors.Is(err, context.Canceled):
			return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorCanceled, err)
		default:
			return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorUnavailable, err)
		}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil {
		return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidResponse, err)
	}
	if len(body) > maximumResponseBytes {
		return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidResponse, errors.New("endpoint response exceeds size limit"))
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return statusError(response.StatusCode, response.Header.Get("Retry-After"))
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidResponse, errors.New("endpoint returned malformed JSON"))
	}
	return nil
}

func statusError(status int, retryAfter string) error {
	kind := blockade.ProviderAdapterErrorUnavailable
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		kind = blockade.ProviderAdapterErrorAuthentication
	case status == http.StatusTooManyRequests:
		kind = blockade.ProviderAdapterErrorRateLimit
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		kind = blockade.ProviderAdapterErrorTimeout
	case status >= 400 && status < 500:
		kind = blockade.ProviderAdapterErrorInvalidRequest
	}
	failure := blockade.NewProviderAdapterError(kind, fmt.Errorf("endpoint returned HTTP %d", status))
	failure.RetryAfter = parseRetryAfter(retryAfter)
	return failure
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 && seconds <= int64(maximumRetryAfter/time.Second) {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delay := time.Until(when)
	if delay < 0 || delay > maximumRetryAfter {
		return 0
	}
	return delay
}
