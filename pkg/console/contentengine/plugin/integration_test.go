package contentkitplugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"console/contentengine"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"jangolova/contentkit"
)

// buildProgram compiles a program of this workspace by import path.
func buildProgram(t *testing.T, importPath string) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "program")
	build := exec.Command("go", "build", "-o", output, importPath)
	if combined, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", importPath, err, combined)
	}
	return output
}

// installProgram installs a built program as an ext package serving the
// content.provider contract under name.
func installProgram(t *testing.T, packages *store.Store, name, program string) store.Installed {
	t.Helper()
	data, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "provider"), data, 0o700); err != nil {
		t.Fatal(err)
	}
	operations := make([]plugin.Operation, 0, len(contentkit.ProviderOperations))
	for _, operation := range contentkit.ProviderOperations {
		operations = append(operations, plugin.Operation{Name: operation})
	}
	contract := plugin.Contract{ContractRef: plugin.ContractRef{Name: contentkit.ProviderContract, Version: contentkit.ProviderContractVersion}, Operations: operations}
	manifest, err := packagekit.ExecutableManifest(name, "1.0.0", []plugin.Contract{contract}, filepath.Join(source, "provider"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteManifest(source, manifest); err != nil {
		t.Fatal(err)
	}
	installed, err := packages.Install(source)
	if err != nil {
		t.Fatal(err)
	}
	return installed
}

func registryFor(t *testing.T, installed ...store.Installed) *contentengine.ProviderAdapterRegistry {
	t.Helper()
	registry := contentengine.NewProviderAdapterRegistry()
	for _, item := range installed {
		if err := RegisterWithRuntimeRoot(registry, item, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func TestFixtureProvidersServeObservationAndInterpretation(t *testing.T) {
	packages, err := store.New(t.TempDir(), store.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	fixture := buildProgram(t, "cymonkey/tests/plugin-fixture")
	registry := registryFor(t,
		installProgram(t, packages, "vision-fixture", fixture),
		installProgram(t, packages, "content-fixture", fixture))
	resolver := contentengine.EnvironmentSecretResolver{}

	model := contentkit.InstalledModel{
		Manifest: contentkit.ModelManifest{
			APIVersion: contentkit.ModelManifestAPIVersion, Name: "fixture", Version: "1.0.0", Provider: "vision-fixture",
			Capabilities: []string{contentkit.CapabilityTextInterpret},
			Artifacts:    []contentkit.ModelArtifact{{Role: "weights", File: "weights.gguf", SHA256: strings.Repeat("0", 64)}},
		},
		Directory: t.TempDir(),
	}
	config := contentengine.ProviderAdapterConfig{ID: "vision", Provider: "vision-fixture"}
	if err := contentengine.InspectConfiguredProviderModel(context.Background(), config, model, registry, resolver); err != nil {
		t.Fatalf("model inspection before startup: %v", err)
	}
	backend, err := contentengine.StartConfiguredProviderAdapter(context.Background(), config, registry, resolver)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := backend.Observe(context.Background(), contentkit.ObserveRequest{Image: []byte("image"), RequestID: "request-1"})
	if err != nil || observed.RequestID != "request-1" {
		t.Fatalf("observation = %#v %v", observed, err)
	}
	interpreter, ok := backend.(contentkit.ContentEngine)
	if !ok {
		t.Fatal("provider has no content interpretation interface")
	}
	for _, request := range []contentkit.InterpretRequest{
		{RequestID: "text-1", Parts: []contentkit.ContentPart{{Kind: "text", Text: "Summarize this note"}}},
		{RequestID: "image-1", Parts: []contentkit.ContentPart{{Kind: "image", MediaType: "image/png", Data: []byte("image")}}},
		{RequestID: "audio-1", Parts: []contentkit.ContentPart{{Kind: "audio", MediaType: "audio/wav", Data: []byte("audio")}}},
	} {
		interpreted, err := interpreter.Interpret(context.Background(), request)
		if err != nil || interpreted.RequestID != request.RequestID || len(interpreted.Findings) != 1 {
			t.Fatalf("interpretation = %#v %v", interpreted, err)
		}
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	contentOnly, err := contentengine.StartConfiguredProviderAdapter(context.Background(), contentengine.ProviderAdapterConfig{ID: "content", Provider: "content-fixture"}, registry, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contentOnly.Observe(context.Background(), contentkit.ObserveRequest{Image: []byte("image")}); !errors.Is(err, contentkit.ErrModalityUnsupported) {
		t.Fatalf("content-only provider image call = %v", err)
	}
	for _, part := range []contentkit.ContentPart{
		{Kind: "text", Text: "Summarize"},
		{Kind: "audio", MediaType: "audio/wav", Data: []byte("sound")},
	} {
		response, err := contentOnly.(contentkit.ContentEngine).Interpret(context.Background(), contentkit.InterpretRequest{Parts: []contentkit.ContentPart{part}})
		if err != nil || len(response.Findings) != 1 {
			t.Fatalf("content-only provider %s = %#v %v", part.Kind, response, err)
		}
	}
	if err := contentOnly.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneOnnxProviderRejectsAWrongModelBeforeStarting(t *testing.T) {
	packages, err := store.New(t.TempDir(), store.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	registry := registryFor(t, installProgram(t, packages, "onnx", buildProgram(t, "cymonkey-content-plugin-onnx")))
	model := contentkit.InstalledModel{Manifest: contentkit.ModelManifest{
		APIVersion: contentkit.ModelManifestAPIVersion, Name: "detector", Version: "1.0.0", Provider: "onnx",
		Capabilities: []string{contentkit.CapabilityObjectDetect},
		Artifacts:    []contentkit.ModelArtifact{{Role: "yolo", File: "detector.gguf", SHA256: strings.Repeat("0", 64)}},
	}, Directory: t.TempDir()}
	err = contentengine.InspectConfiguredProviderModel(context.Background(), contentengine.ProviderAdapterConfig{ID: "onnx", Provider: "onnx"}, model, registry, nil)
	if kind, ok := contentkit.ProviderAdapterErrorKindOf(err); !ok || kind != contentkit.ProviderAdapterErrorInvalidRequest {
		t.Fatalf("ONNX provider inspection error = %v, kind = %q", err, kind)
	}
}
