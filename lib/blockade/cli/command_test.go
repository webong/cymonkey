package blockadecli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"blockade"
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

type cliContentAdapter struct {
	request blockade.ProviderAdapterInterpretRequest
}

func (*cliContentAdapter) Observe(context.Context, blockade.ProviderAdapterObserveRequest) (blockade.ProviderAdapterObserveResponse, error) {
	return blockade.ProviderAdapterObserveResponse{}, blockade.ErrModalityUnsupported
}
func (a *cliContentAdapter) Interpret(_ context.Context, request blockade.ProviderAdapterInterpretRequest) (blockade.ProviderAdapterInterpretResponse, error) {
	a.request = request
	return blockade.ProviderAdapterInterpretResponse{APIVersion: blockade.ProviderAdapterAPIVersion,
		Response: blockade.InterpretResponse{APIVersion: blockade.ContentAPIVersion, RequestID: request.Request.RequestID,
			Findings: []blockade.ContentFinding{{Kind: "summary", Text: "fixture interpretation", SourceParts: []int{0, 1}}}}}, nil
}
func (*cliContentAdapter) Capabilities(context.Context) (blockade.ProviderAdapterCapabilities, error) {
	return blockade.ProviderAdapterCapabilities{APIVersion: blockade.ProviderAdapterAPIVersion, Capabilities: []string{blockade.CapabilityMultimodalInterpret}}, nil
}
func (*cliContentAdapter) Health(context.Context) (blockade.ProviderAdapterHealth, error) {
	return blockade.ProviderAdapterHealth{APIVersion: blockade.ProviderAdapterAPIVersion, Ready: true}, nil
}
func (*cliContentAdapter) Close() error { return nil }

func TestInterpretCommandPassesTextAndAudioToProvider(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "blockade.yaml")
	audioPath := filepath.Join(dir, "speech.wav")
	if err := os.WriteFile(configPath, []byte("apiVersion: blockade.config/v1alpha1\nproviderAdapters:\n  - id: content-fixture\n    kind: fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioPath, []byte("audio bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := &cliContentAdapter{}
	registry := blockade.NewProviderAdapterRegistry()
	if err := registry.Register("fixture", func(context.Context, blockade.ProviderAdapterRuntime) (blockade.ProviderAdapter, error) {
		return adapter, nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunWithProviderAdapters([]string{"interpret", "--config", configPath, "--text", "Agenda", "--audio", audioPath, "--audio-type", "audio/wav"}, &output, &bytes.Buffer{}, registry, nil); err != nil {
		t.Fatal(err)
	}
	var response blockade.InterpretResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Findings) != 1 || response.Findings[0].Text != "fixture interpretation" || len(adapter.request.Request.Parts) != 2 || adapter.request.Request.Parts[0].Text != "Agenda" || string(adapter.request.Request.Parts[1].Data) != "audio bytes" {
		t.Fatalf("CLI interpretation = %#v, request = %#v", response, adapter.request)
	}
}
