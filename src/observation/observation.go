// Package observation coordinates display capture with content interpretation
// through Cymonkey's public service contracts.
package observation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"jangolova/contentkit/public"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const ObservationAPIVersion = "content.observation/v1"
const CaptureServiceContract = "display.interaction"
const InterpretationServiceContract = "content.interpretation"

type ServiceConfig struct {
	Endpoint         string
	TokenEnvironment string
	Token            string
}

type Config struct {
	Capture        ServiceConfig
	Interpretation ServiceConfig
}

// ObservationRequest is Cymonkey's cross-subsystem workflow request. It is
// intentionally distinct from either composed service's own request.
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
	Observation   public.ObservationResult `json:"observation"`
}

type ObservationCoordinator struct {
	config              Config
	token               string
	interpretationToken string
	httpClient          *http.Client
	captureFn           func(context.Context, ObservationRequest) ([]byte, error)
}

func NewObservationCoordinator(config Config) (*ObservationCoordinator, error) {
	if err := validateService(CaptureServiceContract, config.Capture); err != nil {
		return nil, err
	}
	if err := validateService(InterpretationServiceContract, config.Interpretation); err != nil {
		return nil, err
	}
	token := strings.TrimSpace(os.Getenv(config.Capture.TokenEnvironment))
	if token == "" {
		return nil, fmt.Errorf("Cymonkey capture token environment %q is not set", config.Capture.TokenEnvironment)
	}
	interpretationToken, err := resolveServiceToken(config.Interpretation)
	if err != nil {
		return nil, err
	}
	return &ObservationCoordinator{config: config, token: token, interpretationToken: interpretationToken, httpClient: &http.Client{Timeout: 35 * time.Second}}, nil
}

// NewObservationCoordinatorWithCapture coordinates a capture already available
// to the Cymonkey host with content interpretation, without another capture.
func NewObservationCoordinatorWithCapture(config Config, capture func(context.Context, ObservationRequest) ([]byte, error)) (*ObservationCoordinator, error) {
	if err := validateService(InterpretationServiceContract, config.Interpretation); err != nil {
		return nil, err
	}
	if capture == nil {
		return nil, errors.New("observation capture function is required")
	}
	interpretationToken, err := resolveServiceToken(config.Interpretation)
	if err != nil {
		return nil, err
	}
	return &ObservationCoordinator{config: config, interpretationToken: interpretationToken, captureFn: capture, httpClient: &http.Client{Timeout: 35 * time.Second}}, nil
}

func (c *ObservationCoordinator) Observe(ctx context.Context, request ObservationRequest) (ObservationResponse, error) {
	if c == nil {
		return ObservationResponse{}, errors.New("Cymonkey observation coordinator is required")
	}
	if strings.TrimSpace(request.InstanceID) == "" {
		return ObservationResponse{}, errors.New("Cymonkey observation instanceId is required")
	}
	var image []byte
	var err error
	if c.captureFn != nil {
		image, err = c.captureFn(ctx, request)
	} else {
		image, err = c.capture(ctx, request)
	}
	if err != nil {
		return ObservationResponse{}, err
	}
	observation, err := (public.ServiceClient{BaseURL: c.config.Interpretation.Endpoint, HTTPClient: c.httpClient, BearerToken: c.interpretationToken}).Observe(ctx, public.ObservationRequest{APIVersion: public.ObservationRequestVersion, Image: image, Prompt: request.Prompt})
	if err != nil {
		return ObservationResponse{}, fmt.Errorf("interpret captured content: %w", err)
	}
	return ObservationResponse{
		APIVersion: ObservationAPIVersion, InstanceID: request.InstanceID,
		CapturedAt: time.Now().UTC(), CaptureAction: "window.screenshot", Observation: observation,
	}, nil
}

func resolveServiceToken(config ServiceConfig) (string, error) {
	if token := strings.TrimSpace(config.Token); token != "" {
		return token, nil
	}
	name := strings.TrimSpace(config.TokenEnvironment)
	if name == "" {
		return "", nil
	}
	token := strings.TrimSpace(os.Getenv(name))
	if token == "" {
		return "", fmt.Errorf("Cymonkey interpretation token environment %q is not set", name)
	}
	return token, nil
}

func validateService(contract string, config ServiceConfig) error {
	parsed, err := url.Parse(strings.TrimSpace(config.Endpoint))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("Cymonkey %s endpoint must be an HTTP URL", contract)
	}
	return nil
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
		return nil, fmt.Errorf("encode display capture request: %w", err)
	}
	endpoint := strings.TrimRight(c.config.Capture.Endpoint, "/") + "/v1/display-instances/" + url.PathEscape(request.InstanceID) + "/call"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create display capture request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.token)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("capture from display service: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("display capture returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 17<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode display capture response: %w", err)
	}
	var result struct {
		PNGBase64 string `json:"pngBase64"`
	}
	if err := json.Unmarshal(payload.Result, &result); err != nil || strings.TrimSpace(result.PNGBase64) == "" {
		return nil, errors.New("display screenshot returned no PNG")
	}
	image, err := base64.StdEncoding.DecodeString(result.PNGBase64)
	if err != nil || len(image) == 0 || len(image) > 16<<20 {
		return nil, errors.New("display screenshot returned an invalid PNG")
	}
	return image, nil
}
