// Package engineprovider implements the provider-neutral interaction-engine
// contract. Jangolova is one implementation and Xallet is one supported
// target provider.
package engineprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client is an HTTP client for the engine provider API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient returns a Client that talks to the engine provider at baseURL,
// authenticating with the given bearer token.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{},
	}
}

// Connect sends a POST /v1/instances to create an interaction instance.
func (c *Client) Connect(ctx context.Context, req ConnectRequest) (*Instance, error) {
	var result Instance
	if err := c.do(ctx, http.MethodPost, "/v1/instances", req, &result, http.StatusCreated); err != nil {
		return nil, err
	}
	return &result, nil
}

// Delete sends a DELETE /v1/instances/{id} to disconnect an instance.
func (c *Client) Delete(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/instances/"+url.PathEscape(id), nil, nil, http.StatusOK, http.StatusNoContent)
}

// Call sends a POST /v1/instances/{id}/call to invoke a method on an instance.
func (c *Client) Call(ctx context.Context, id, method string, params json.RawMessage) (*CallResponse, error) {
	req := CallRequest{Method: method, Params: params}
	var result CallResponse
	if err := c.do(ctx, http.MethodPost, "/v1/instances/"+url.PathEscape(id)+"/call", req, &result, http.StatusOK); err != nil {
		return nil, err
	}
	return &result, nil
}

// Get sends a GET /v1/instances/{id} to inspect an instance.
func (c *Client) Get(ctx context.Context, id string) (*Instance, error) {
	var result Instance
	if err := c.do(ctx, http.MethodGet, "/v1/instances/"+url.PathEscape(id), nil, &result, http.StatusOK); err != nil {
		return nil, err
	}
	return &result, nil
}

// Engines sends a GET /v1/engines to list available engine adapters.
func (c *Client) Engines(ctx context.Context) ([]EngineDescriptor, error) {
	wrapper := struct {
		APIVersion string             `json:"apiVersion"`
		Engines    []EngineDescriptor `json:"engines"`
	}{}
	if err := c.do(ctx, http.MethodGet, "/v1/engines", nil, &wrapper, http.StatusOK); err != nil {
		return nil, err
	}
	return wrapper.Engines, nil
}

// Reconcile sends a POST /v1/reconcile to declare desired instance state.
func (c *Client) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResponse, error) {
	var result ReconcileResponse
	if err := c.do(ctx, http.MethodPost, "/v1/reconcile", req, &result, http.StatusOK); err != nil {
		return nil, err
	}
	return &result, nil
}

// Health sends a GET /healthz to check provider health.
func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/healthz", nil, nil, http.StatusOK)
}

func (c *Client) do(ctx context.Context, method, path string, body, result any, expectStatus ...int) error {
	var requestBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode engine-provider request: %w", err)
		}
		requestBody = bytes.NewReader(raw)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, requestBody)
	if err != nil {
		return fmt.Errorf("create engine-provider request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("engine-provider request: %w", err)
	}
	defer response.Body.Close()

	// Accept any of the expected status codes. If none are specified, accept 200.
	matched := len(expectStatus) == 0 && response.StatusCode == http.StatusOK
	if !matched {
		for _, code := range expectStatus {
			if response.StatusCode == code {
				matched = true
				break
			}
		}
	}
	if !matched {
		responseBody, _ := io.ReadAll(response.Body)
		if response.StatusCode == http.StatusNotFound {
			return errors.New("engine-provider instance was not found")
		}
		return fmt.Errorf("unexpected engine-provider status %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	if result == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return fmt.Errorf("decode engine-provider response: %w", err)
	}
	return nil
}
