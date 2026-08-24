package blockade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	WorkerPool *WorkerPool
}

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
	if c.WorkerPool != nil {
		return c.WorkerPool.Observe(ctx, request)
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
