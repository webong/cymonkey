package blockadecli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"blockade"
)

type cliProviderAdapter struct {
	request   blockade.ProviderAdapterObserveRequest
	inspected *blockade.InstalledModel
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
	config := []byte("apiVersion: blockade.config/v1alpha1\nproviders:\n  - id: hosted-fixture\n    provider: fixture\n")
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

func TestSelectProviderByID(t *testing.T) {
	config := blockade.Config{APIVersion: blockade.ConfigAPIVersion, Providers: []blockade.ProviderAdapterConfig{{ID: "fixture", Provider: "fixture"}}}
	selected, err := selectProvider(config, "fixture")
	if err != nil || selected.ID != "fixture" {
		t.Fatalf("selected = %#v, err = %v", selected, err)
	}
}

func TestModelInstallUsesProviderInspectionAndStartsWithInstalledModel(t *testing.T) {
	dir := t.TempDir()
	modelSource := filepath.Join(dir, "source")
	if err := os.Mkdir(modelSource, 0700); err != nil {
		t.Fatal(err)
	}
	weights := []byte("fixture weights")
	digest := sha256.Sum256(weights)
	if err := os.WriteFile(filepath.Join(modelSource, "weights.gguf"), weights, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := blockade.ModelManifest{
		APIVersion: blockade.ModelManifestAPIVersion, Name: "fixture-model", Version: "1.0.0", Provider: "fixture",
		Capabilities: []string{blockade.CapabilityTextInterpret},
		Artifacts:    []blockade.ModelArtifact{{Role: "weights", File: "weights.gguf", SHA256: hex.EncodeToString(digest[:])}},
	}
	manifestData, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(modelSource, "model.json")
	if err := os.WriteFile(manifestPath, manifestData, 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "blockade.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: blockade.config/v1alpha1\nproviders:\n  - id: fixture\n    provider: fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	modelRoot := filepath.Join(dir, "models")
	adapter := &cliProviderAdapter{}
	var runtime blockade.ProviderAdapterRuntime
	startCalls := 0
	registry := blockade.NewProviderAdapterRegistry()
	if err := registry.RegisterWithModelInspector("fixture", func(_ context.Context, value blockade.ProviderAdapterRuntime) (blockade.ProviderAdapter, error) {
		startCalls++
		runtime = value
		return adapter, nil
	}, func(_ context.Context, _ blockade.ProviderAdapterRuntime, model blockade.InstalledModel) error {
		adapter.inspected = &model
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunWithProviderAdapters([]string{"model", "install", "--manifest", manifestPath, "--config", configPath, "--root", modelRoot}, &output, &bytes.Buffer{}, registry, nil); err != nil {
		t.Fatal(err)
	}
	if adapter.inspected == nil || adapter.inspected.Manifest.Name != "fixture-model" {
		t.Fatalf("inspected model = %#v", adapter.inspected)
	}
	if startCalls != 0 {
		t.Fatalf("model inspection started the inference provider %d time(s)", startCalls)
	}
	engine, err := blockade.StartConfiguredProviderAdapter(context.Background(), blockade.ProviderAdapterConfig{
		ID: "fixture", Provider: "fixture", Model: "fixture-model@1.0.0", ModelRoot: modelRoot,
	}, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if startCalls != 1 {
		t.Fatalf("provider starts = %d, want 1", startCalls)
	}
	if runtime.Model == nil || runtime.Model.Manifest.Name != "fixture-model" || string(mustReadFile(t, filepath.Join(runtime.Model.Directory, "weights.gguf"))) != string(weights) {
		t.Fatalf("provider runtime model = %#v", runtime.Model)
	}
	output.Reset()
	if err := RunWithProviderAdapters([]string{"model", "list", "--root", modelRoot}, &output, &bytes.Buffer{}, registry, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"name":"fixture-model"`)) {
		t.Fatalf("model list = %s", output.String())
	}
	installedDirectory := runtime.Model.Directory
	output.Reset()
	if err := RunWithProviderAdapters([]string{"model", "remove", "--provider", "fixture", "--model", "fixture-model@1.0.0", "--root", modelRoot}, &output, &bytes.Buffer{}, registry, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installedDirectory); !os.IsNotExist(err) {
		t.Fatalf("removed model still exists: %v", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
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
	if err := os.WriteFile(configPath, []byte("apiVersion: blockade.config/v1alpha1\nproviders:\n  - id: content-fixture\n    provider: fixture\n"), 0o600); err != nil {
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
