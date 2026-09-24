package blockadecli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cymonkey/lib/blockade"
)

type cliProviderAdapter struct {
	request blockade.ProviderAdapterObserveRequest
}

func (a *cliProviderAdapter) Observe(_ context.Context, request blockade.ProviderAdapterObserveRequest) (blockade.ProviderAdapterObserveResponse, error) {
	a.request = request
	return blockade.ProviderAdapterObserveResponse{
		APIVersion: blockade.ProviderAdapterAPIVersion,
		Response: blockade.ObserveResponse{
			APIVersion: blockade.APIVersion,
			RequestID:  request.Request.RequestID,
			Observations: []blockade.Observation{{
				Kind: "description", Label: "fixture description", Confidence: 0.8,
				Region: blockade.Region{Width: 10, Height: 10},
			}},
		},
	}, nil
}

func (*cliProviderAdapter) Capabilities(context.Context) (blockade.ProviderAdapterCapabilities, error) {
	return blockade.ProviderAdapterCapabilities{
		APIVersion:   blockade.ProviderAdapterAPIVersion,
		Capabilities: []string{"image.describe"},
	}, nil
}

func (*cliProviderAdapter) Health(context.Context) (blockade.ProviderAdapterHealth, error) {
	return blockade.ProviderAdapterHealth{APIVersion: blockade.ProviderAdapterAPIVersion, Ready: true}, nil
}

func (*cliProviderAdapter) Close() error { return nil }

func TestObserveCommandUsesConfiguredProviderAdapter(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "blockade.yaml")
	imagePath := filepath.Join(dir, "image.png")
	config := []byte("apiVersion: blockade.config/v1alpha1\nproviderAdapters:\n  - id: hosted-fixture\n    kind: fixture\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	image := []byte("fixture-image")
	if err := os.WriteFile(imagePath, image, 0o600); err != nil {
		t.Fatal(err)
	}

	adapter := &cliProviderAdapter{}
	registry := blockade.NewProviderAdapterRegistry()
	if err := registry.Register("fixture", func(context.Context, blockade.ProviderAdapterRuntime) (blockade.ProviderAdapter, error) {
		return adapter, nil
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := RunWithProviderAdapters([]string{
		"observe", "--config", configPath, "--inference", "hosted-fixture", "--image", imagePath, "--prompt", "describe",
	}, &stdout, &bytes.Buffer{}, registry, blockade.EnvironmentSecretResolver{}); err != nil {
		t.Fatal(err)
	}
	var response blockade.ObserveResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if err := blockade.ValidateObserveResponse(response); err != nil {
		t.Fatal(err)
	}
	if adapter.request.Request.Prompt != "describe" || string(adapter.request.Request.Image) != string(image) {
		t.Fatalf("adapter request = %#v", adapter.request)
	}
}

func TestSelectInferenceRejectsAmbiguousFlags(t *testing.T) {
	config := blockade.Config{APIVersion: blockade.ConfigAPIVersion, ProviderAdapters: []blockade.ProviderAdapterConfig{{ID: "fixture", Kind: "fixture"}}}
	if _, err := selectInference(config, "fixture", "fixture"); err == nil {
		t.Fatal("expected --inference and --engine together to fail")
	}
}
