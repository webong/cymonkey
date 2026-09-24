package cymonkeyhost

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"cymonkey/lib/blockade"
)

const ObservationAPIVersion = "cymonkey.observation/v1alpha1"

// ObservationRequest is Cymonkey's cross-subsystem workflow request. It is
// intentionally distinct from Jangolova's interaction calls and Blockade's
// pixel-only contract.
type ObservationRequest struct {
	InstanceID string `json:"instanceId"`
	Prompt     string `json:"prompt,omitempty"`
	FullPage   bool   `json:"fullPage,omitempty"`
	ApprovalID string `json:"approvalId,omitempty"`
}

type ObservationResponse struct {
	APIVersion    string                   `json:"apiVersion"`
	InstanceID    string                   `json:"instanceId"`
	CapturedAt    time.Time                `json:"capturedAt"`
	CaptureAction string                   `json:"captureAction"`
	Observation   blockade.ObserveResponse `json:"observation"`
}

type ObservationCoordinator struct {
	config     ObservationConfig
	token      string
	httpClient *http.Client
}

func NewObservationCoordinator(config ObservationConfig) (*ObservationCoordinator, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	token := strings.TrimSpace(os.Getenv(config.Jangolova.TokenEnvironment))
	if token == "" {
		return nil, fmt.Errorf("Cymonkey observation Jangolova token environment %q is not set", config.Jangolova.TokenEnvironment)
	}
	return &ObservationCoordinator{config: config, token: token, httpClient: &http.Client{Timeout: 35 * time.Second}}, nil
}

func (c *ObservationCoordinator) Observe(ctx context.Context, request ObservationRequest) (ObservationResponse, error) {
	if c == nil {
		return ObservationResponse{}, errors.New("Cymonkey observation coordinator is required")
	}
	if strings.TrimSpace(request.InstanceID) == "" {
		return ObservationResponse{}, errors.New("Cymonkey observation instanceId is required")
	}
	image, err := c.capture(ctx, request)
	if err != nil {
		return ObservationResponse{}, err
	}
	observation, err := (blockade.Client{BaseURL: c.config.Blockade.Endpoint, HTTPClient: c.httpClient}).Observe(ctx, blockade.ObserveRequest{Image: image, Prompt: request.Prompt})
	if err != nil {
		return ObservationResponse{}, fmt.Errorf("observe with Blockade: %w", err)
	}
	return ObservationResponse{
		APIVersion: ObservationAPIVersion, InstanceID: request.InstanceID,
		CapturedAt: time.Now().UTC(), CaptureAction: "window.screenshot", Observation: observation,
	}, nil
}

func (c *ObservationCoordinator) capture(ctx context.Context, request ObservationRequest) ([]byte, error) {
	body, err := json.Marshal(map[string]any{
		"method": "act",
		"params": map[string]any{
			"name":  "window.screenshot",
			"input": map[string]any{"fullPage": request.FullPage},
		},
		"approvalId": request.ApprovalID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode Jangolova capture request: %w", err)
	}
	endpoint := strings.TrimRight(c.config.Jangolova.Endpoint, "/") + "/v1/instances/" + url.PathEscape(request.InstanceID) + "/call"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Jangolova capture request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.token)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("capture from Jangolova: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Jangolova capture returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 17<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode Jangolova capture response: %w", err)
	}
	var result struct {
		PNGBase64 string `json:"pngBase64"`
	}
	if err := json.Unmarshal(payload.Result, &result); err != nil || strings.TrimSpace(result.PNGBase64) == "" {
		return nil, errors.New("Jangolova window.screenshot returned no PNG")
	}
	image, err := base64.StdEncoding.DecodeString(result.PNGBase64)
	if err != nil || len(image) == 0 || len(image) > 16<<20 {
		return nil, errors.New("Jangolova window.screenshot returned an invalid PNG")
	}
	return image, nil
}
