package contentbridge

import (
	"bytes"
	"context"
	"cymonkey/src/contracts"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"jangolova/contentkit"
	"net/http"
	"strings"
	"time"
)

var ErrModalityUnsupported = errors.New("requested content modality is unsupported")

type ServiceClient struct {
	BaseURL     string
	HTTPClient  *http.Client
	BearerToken string
}

type ServiceHealth struct {
	Status string `json:"status"`
	Plugin string `json:"plugin"`
}

type ServiceCapabilities struct {
	APIVersion   string   `json:"apiVersion"`
	Plugin       string   `json:"plugin"`
	Capabilities []string `json:"capabilities"`
}

func ValidateRequest(request contracts.ContentRequest) error {
	if request.APIVersion == "" {
		request.APIVersion = contracts.ContentAPIVersion
	}
	if request.APIVersion != contracts.ContentAPIVersion {
		return fmt.Errorf("content apiVersion must be %q", contracts.ContentAPIVersion)
	}
	if err := contentkit.ValidateInterpretRequest(ToLibraryRequest(request)); err != nil {
		return publicError(err)
	}
	return nil
}

func (client ServiceClient) Interpret(ctx context.Context, request contracts.ContentRequest) (contracts.ContentResponse, error) {
	if request.APIVersion == "" {
		request.APIVersion = contracts.ContentAPIVersion
	}
	if err := ValidateRequest(request); err != nil {
		return contracts.ContentResponse{}, err
	}
	var response contracts.ContentResponse
	if err := client.call(ctx, "/v1/interpret", request, &response); err != nil {
		return contracts.ContentResponse{}, err
	}
	if response.APIVersion != contracts.ContentAPIVersion {
		return contracts.ContentResponse{}, fmt.Errorf("content service returned unsupported apiVersion %q", response.APIVersion)
	}
	return response, nil
}

func (client ServiceClient) Observe(ctx context.Context, request contracts.ObservationRequest) (contracts.ObservationResult, error) {
	if request.APIVersion == "" {
		request.APIVersion = contracts.ObservationRequestVersion
	}
	if request.APIVersion != contracts.ObservationRequestVersion {
		return contracts.ObservationResult{}, fmt.Errorf("observation apiVersion must be %q", contracts.ObservationRequestVersion)
	}
	if len(request.Image) == 0 {
		return contracts.ObservationResult{}, errors.New("observation image is required")
	}
	var response contracts.ObservationResult
	if err := client.call(ctx, "/v1/observe", request, &response); err != nil {
		return contracts.ObservationResult{}, err
	}
	if response.APIVersion != contracts.ObservationAPIVersion {
		return contracts.ObservationResult{}, fmt.Errorf("content service returned unsupported observation apiVersion %q", response.APIVersion)
	}
	return response, nil
}

func (client ServiceClient) Health(ctx context.Context) (ServiceHealth, error) {
	var result ServiceHealth
	status, err := client.get(ctx, "/healthz", &result)
	if err != nil {
		return ServiceHealth{}, err
	}
	if status != http.StatusOK || result.Status != "ok" {
		return result, fmt.Errorf("content service health returned HTTP %d with status %q", status, result.Status)
	}
	return result, nil
}

func (client ServiceClient) Capabilities(ctx context.Context) (ServiceCapabilities, error) {
	var result ServiceCapabilities
	status, err := client.get(ctx, "/capabilities", &result)
	if err != nil {
		return ServiceCapabilities{}, err
	}
	if status/100 != 2 {
		return ServiceCapabilities{}, fmt.Errorf("content service capabilities returned HTTP %d", status)
	}
	if result.APIVersion != contracts.ContentServiceVersion {
		return ServiceCapabilities{}, fmt.Errorf("content service returned unsupported apiVersion %q", result.APIVersion)
	}
	return result, nil
}

func (client ServiceClient) call(ctx context.Context, path string, request, response any) error {
	if strings.TrimSpace(client.BaseURL) == "" {
		return errors.New("content service endpoint is required")
	}
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(client.BaseURL, "/")+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if client.BearerToken != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+client.BearerToken)
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("call content service: %w", err)
	}
	defer httpResponse.Body.Close()
	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, 16<<20))
	if err != nil {
		return err
	}
	if httpResponse.StatusCode/100 != 2 {
		if httpResponse.StatusCode == http.StatusNotImplemented {
			return ErrModalityUnsupported
		}
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &failure) == nil && failure.Error != "" {
			return fmt.Errorf("content service returned HTTP %d: %s", httpResponse.StatusCode, failure.Error)
		}
		return fmt.Errorf("content service returned HTTP %d", httpResponse.StatusCode)
	}
	if err := json.Unmarshal(body, response); err != nil {
		return fmt.Errorf("decode content service response: %w", err)
	}
	return nil
}

func (client ServiceClient) get(ctx context.Context, path string, response any) (int, error) {
	if strings.TrimSpace(client.BaseURL) == "" {
		return 0, errors.New("content service endpoint is required")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(client.BaseURL, "/")+path, nil)
	if err != nil {
		return 0, err
	}
	if client.BearerToken != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+client.BearerToken)
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		return 0, fmt.Errorf("call content service: %w", err)
	}
	defer httpResponse.Body.Close()
	data, err := io.ReadAll(io.LimitReader(httpResponse.Body, (1<<20)+1))
	if err != nil {
		return httpResponse.StatusCode, err
	}
	if len(data) > 1<<20 {
		return httpResponse.StatusCode, errors.New("content service response exceeds 1 MiB")
	}
	if err := json.Unmarshal(data, response); err != nil {
		return httpResponse.StatusCode, fmt.Errorf("decode content service response: %w", err)
	}
	return httpResponse.StatusCode, nil
}
