package contentcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"jangolova/contentkit"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cliProviderAdapter struct {
	request   contentkit.ProviderAdapterObserveRequest
	inspected *contentkit.InstalledModel
}

func RunWithProviderAdapters(args []string, stdout, stderr io.Writer, registry *contentkit.ProviderAdapterRegistry, resolver contentkit.SecretResolver) error {
	return (CommandEngine{Invocation: "cymy content", Registry: registry, Resolver: resolver}).Run(args, stdout, stderr)
}

func TestCommandEngineUsesHostInvocation(t *testing.T) {
	var output bytes.Buffer
	if err := (CommandEngine{Invocation: "host content", Registry: contentkit.NewProviderAdapterRegistry()}).Run([]string{"help"}, io.Discard, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Usage: host content <command>") {
		t.Fatalf("help = %q", output.String())
	}
	if err := (CommandEngine{}).Run([]string{"help"}, io.Discard, io.Discard); err == nil {
		t.Fatal("expected a missing invocation error")
	}
}

func (a *cliProviderAdapter) Observe(_ context.Context, request contentkit.ProviderAdapterObserveRequest) (contentkit.ProviderAdapterObserveResponse, error) {
	a.request = request
	return contentkit.ProviderAdapterObserveResponse{
		APIVersion: contentkit.ProviderAdapterAPIVersion,
		Response: contentkit.ObserveResponse{
			APIVersion: contentkit.APIVersion,
			RequestID:  request.Request.RequestID,
			Observations: []contentkit.Observation{{
				Kind: "description", Label: "fixture description", Confidence: 0.8,
				Region: contentkit.Region{Width: 10, Height: 10},
			}},
		},
	}, nil
}

func (*cliProviderAdapter) Capabilities(context.Context) (contentkit.ProviderAdapterCapabilities, error) {
	return contentkit.ProviderAdapterCapabilities{
		APIVersion:   contentkit.ProviderAdapterAPIVersion,
		Capabilities: []string{"image.describe"},
	}, nil
}

func (*cliProviderAdapter) Health(context.Context) (contentkit.ProviderAdapterHealth, error) {
	return contentkit.ProviderAdapterHealth{APIVersion: contentkit.ProviderAdapterAPIVersion, Ready: true}, nil
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
	registry := contentkit.NewProviderAdapterRegistry()
	if err := registry.Register("fixture", func(context.Context, contentkit.ProviderAdapterRuntime) (contentkit.ProviderAdapter, error) {
		return adapter, nil
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := RunWithProviderAdapters([]string{
		"observe", "--config", configPath, "--inference", "hosted-fixture", "--image", imagePath, "--prompt", "describe",
	}, &stdout, &bytes.Buffer{}, registry, contentkit.EnvironmentSecretResolver{}); err != nil {
		t.Fatal(err)
	}
	var response contentkit.ObserveResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if err := contentkit.ValidateObserveResponse(response); err != nil {
		t.Fatal(err)
	}
	if adapter.request.Request.Prompt != "describe" || string(adapter.request.Request.Image) != string(image) {
		t.Fatalf("adapter request = %#v", adapter.request)
	}
}

func TestSelectProviderByID(t *testing.T) {
	config := contentkit.Config{APIVersion: contentkit.ConfigAPIVersion, Providers: []contentkit.ProviderAdapterConfig{{ID: "fixture", Provider: "fixture"}}}
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
	manifest := contentkit.ModelManifest{
		APIVersion: contentkit.ModelManifestAPIVersion, Name: "fixture-model", Version: "1.0.0", Provider: "fixture",
		Capabilities: []string{contentkit.CapabilityTextInterpret},
		Artifacts:    []contentkit.ModelArtifact{{Role: "weights", File: "weights.gguf", SHA256: hex.EncodeToString(digest[:])}},
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
	var runtime contentkit.ProviderAdapterRuntime
	startCalls := 0
	registry := contentkit.NewProviderAdapterRegistry()
	if err := registry.RegisterWithModelInspector("fixture", func(_ context.Context, value contentkit.ProviderAdapterRuntime) (contentkit.ProviderAdapter, error) {
		startCalls++
		runtime = value
		return adapter, nil
	}, func(_ context.Context, _ contentkit.ProviderAdapterRuntime, model contentkit.InstalledModel) error {
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
	engine, err := contentkit.StartConfiguredProviderAdapter(context.Background(), contentkit.ProviderAdapterConfig{
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

func TestRuntimeManifestInstallListAndRemove(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "python-tree")
	executable := filepath.Join(source, "bin", "python3")
	if err := os.MkdirAll(filepath.Dir(executable), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(source, "runtime.json")
	runtimeRoot := filepath.Join(dir, "installed")
	registry := contentkit.NewProviderAdapterRegistry()
	var output bytes.Buffer
	if err := RunWithProviderAdapters([]string{
		"runtime", "manifest", "--source", source, "--name", "python", "--version", "3.12.8",
		"--executable", "bin/python3", "--output", manifestPath,
	}, &output, &bytes.Buffer{}, registry, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := contentkit.LoadRuntimeManifest(manifestPath); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := RunWithProviderAdapters([]string{"runtime", "install", "--manifest", manifestPath, "--root", runtimeRoot}, &output, &bytes.Buffer{}, registry, nil); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := RunWithProviderAdapters([]string{"runtime", "list", "--root", runtimeRoot}, &output, &bytes.Buffer{}, registry, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"name":"python"`)) {
		t.Fatalf("runtime list = %s", output.String())
	}
	output.Reset()
	if err := RunWithProviderAdapters([]string{"runtime", "remove", "--name", "python", "--version", "3.12.8", "--root", runtimeRoot}, &output, &bytes.Buffer{}, registry, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := contentkit.ResolveInstalledRuntime(runtimeRoot, "python", "3.12.8"); !os.IsNotExist(err) {
		t.Fatalf("removed runtime resolution = %v", err)
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
	request contentkit.ProviderAdapterInterpretRequest
}

func (*cliContentAdapter) Observe(context.Context, contentkit.ProviderAdapterObserveRequest) (contentkit.ProviderAdapterObserveResponse, error) {
	return contentkit.ProviderAdapterObserveResponse{}, contentkit.ErrModalityUnsupported
}
func (a *cliContentAdapter) Interpret(_ context.Context, request contentkit.ProviderAdapterInterpretRequest) (contentkit.ProviderAdapterInterpretResponse, error) {
	a.request = request
	return contentkit.ProviderAdapterInterpretResponse{APIVersion: contentkit.ProviderAdapterAPIVersion,
		Response: contentkit.InterpretResponse{APIVersion: contentkit.ContentAPIVersion, RequestID: request.Request.RequestID,
			Findings: []contentkit.ContentFinding{{Kind: "summary", Text: "fixture interpretation", SourceParts: []int{0, 1}}}}}, nil
}
func (*cliContentAdapter) Capabilities(context.Context) (contentkit.ProviderAdapterCapabilities, error) {
	return contentkit.ProviderAdapterCapabilities{APIVersion: contentkit.ProviderAdapterAPIVersion, Capabilities: []string{contentkit.CapabilityMultimodalInterpret}}, nil
}
func (*cliContentAdapter) Health(context.Context) (contentkit.ProviderAdapterHealth, error) {
	return contentkit.ProviderAdapterHealth{APIVersion: contentkit.ProviderAdapterAPIVersion, Ready: true}, nil
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
	registry := contentkit.NewProviderAdapterRegistry()
	if err := registry.Register("fixture", func(context.Context, contentkit.ProviderAdapterRuntime) (contentkit.ProviderAdapter, error) {
		return adapter, nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunWithProviderAdapters([]string{"interpret", "--config", configPath, "--text", "Agenda", "--audio", audioPath, "--audio-type", "audio/wav"}, &output, &bytes.Buffer{}, registry, nil); err != nil {
		t.Fatal(err)
	}
	var response contentkit.InterpretResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Findings) != 1 || response.Findings[0].Text != "fixture interpretation" || len(adapter.request.Request.Parts) != 2 || adapter.request.Request.Parts[0].Text != "Agenda" || string(adapter.request.Request.Parts[1].Data) != "audio bytes" {
		t.Fatalf("CLI interpretation = %#v, request = %#v", response, adapter.request)
	}
}

func TestInterpretCommandPassesVideoToProvider(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "blockade.yaml")
	videoPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(configPath, []byte("apiVersion: blockade.config/v1alpha1\nproviders:\n  - id: content-fixture\n    provider: fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoPath, []byte("video bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := &cliContentAdapter{}
	registry := contentkit.NewProviderAdapterRegistry()
	if err := registry.Register("fixture", func(context.Context, contentkit.ProviderAdapterRuntime) (contentkit.ProviderAdapter, error) {
		return adapter, nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunWithProviderAdapters([]string{"interpret", "--config", configPath, "--text", "Describe", "--video", videoPath, "--video-type", "video/mp4"}, &output, &bytes.Buffer{}, registry, nil); err != nil {
		t.Fatal(err)
	}
	if len(adapter.request.Request.Parts) != 2 || adapter.request.Request.Parts[1].Kind != "video" || adapter.request.Request.Parts[1].MediaType != "video/mp4" || string(adapter.request.Request.Parts[1].Data) != "video bytes" {
		t.Fatalf("CLI video request = %#v", adapter.request.Request)
	}
}

func TestEndpointAddTestListAndRemove(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "blockade.yaml")
	t.Setenv("CYMONKEY_CONTENT_CONFIG", configPath)
	adapter := &cliContentAdapter{}
	registry := contentkit.NewProviderAdapterRegistry()
	if err := registry.Register("fixture", func(context.Context, contentkit.ProviderAdapterRuntime) (contentkit.ProviderAdapter, error) {
		return adapter, nil
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunWithProviderAdapters([]string{
		"endpoint", "add", "--id", "local", "--provider", "fixture", "--model", "fixture-v1", "--modalities", "text,audio", "--api-key-env", "FIXTURE_TOKEN",
	}, &output, io.Discard, registry, nil); err != nil {
		t.Fatal(err)
	}
	config, err := contentkit.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.Providers[0].Settings["modalities"] != "text,audio" || config.Providers[0].Secrets["apiKey"].Env != "FIXTURE_TOKEN" {
		t.Fatalf("endpoint config = %#v", config.Providers[0])
	}
	output.Reset()
	if err := RunWithProviderAdapters([]string{"endpoint", "test", "--id", "local"}, &output, io.Discard, registry, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"ready":true`)) {
		t.Fatalf("endpoint test = %s", output.String())
	}
	output.Reset()
	if err := RunWithProviderAdapters([]string{"endpoint", "list"}, &output, io.Discard, registry, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"id":"local"`)) {
		t.Fatalf("endpoint list = %s", output.String())
	}
	if err := RunWithProviderAdapters([]string{"endpoint", "remove", "--id", "local"}, io.Discard, io.Discard, registry, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("removed endpoint config stat = %v", err)
	}
}

func TestEndpointProfileCreatesCompatibleProviderConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "blockade.yaml")
	registry := contentkit.NewProviderAdapterRegistry()
	if err := RunWithProviderAdapters([]string{
		"endpoint", "add", "--config", configPath, "--id", "ollama", "--profile", "ollama", "--model", "qwen3-vl", "--url", "http://127.0.0.1:11434/v1",
	}, io.Discard, io.Discard, registry, nil); err != nil {
		t.Fatal(err)
	}
	config, err := contentkit.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	provider := config.Providers[0]
	if provider.Provider != "openai-compatible" || provider.Settings["profile"] != "ollama" || provider.Settings["baseURL"] != "http://127.0.0.1:11434/v1" {
		t.Fatalf("endpoint provider = %#v", provider)
	}
}

func TestServeRequiresAuthenticationAndTLSOutsideLoopback(t *testing.T) {
	registry := contentkit.NewProviderAdapterRegistry()
	err := RunWithProviderAdapters([]string{"serve", "--bind", "0.0.0.0:8091", "--config", filepath.Join(t.TempDir(), "missing.yaml")}, io.Discard, io.Discard, registry, nil)
	if err == nil || !strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("serve error = %v", err)
	}
}
