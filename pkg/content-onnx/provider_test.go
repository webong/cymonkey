package main

import (
	"contentkit"
	"context"
	"encoding/json"
	"strings"
	"testing"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
)

func TestModelInspectionRejectsWrongArtifactWithoutStarting(t *testing.T) {
	p := &provider{descriptor: plugin.Descriptor{Identity: plugin.Identity{ID: "onnx"}}}
	model := contentkit.InstalledModel{Manifest: contentkit.ModelManifest{
		APIVersion: contentkit.ModelManifestAPIVersion,
		Name:       "detector", Version: "1.0.0", Provider: "onnx",
		Capabilities: []string{contentkit.CapabilityObjectDetect},
		Artifacts:    []contentkit.ModelArtifact{{Role: "yolo", File: "detector.gguf", SHA256: strings.Repeat("0", 64)}},
	}, Directory: t.TempDir()}
	params, err := json.Marshal(map[string]any{"model": model})
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.call(context.Background(), "content.provider.model.inspect", params)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	if result["compatible"] != false || p.engine != nil {
		t.Fatalf("inspection result=%#v engine=%#v", result, p.engine)
	}
}

func TestStartRequiresInstalledModel(t *testing.T) {
	p := &provider{descriptor: plugin.Descriptor{Identity: plugin.Identity{ID: "onnx"}}}
	params, _ := json.Marshal(map[string]any{"id": "detector", "settings": map[string]string{}})
	if _, err := p.call(context.Background(), "content.provider.start", params); err == nil {
		t.Fatal("expected start without model to fail")
	}
}

func TestProviderSettings(t *testing.T) {
	settings, err := parseProviderSettings(map[string]string{
		"executionProviders":  `[{"name":"coreml"},{"name":"cpu"}]`,
		"confidenceThreshold": "0.4",
		"nmsThreshold":        "0.5",
		"runtimeLibrary":      "/opt/onnx/libonnxruntime.dylib",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.executionProviders) != 2 || settings.confidence != 0.4 || settings.iou != 0.5 {
		t.Fatalf("settings = %#v", settings)
	}
	if _, err := parseProviderSettings(map[string]string{"unknown": "value"}); err == nil {
		t.Fatal("expected unknown setting to fail")
	}
}

// Blockade sends "secrets" (empty for an image provider) with every observe
// call, and decodeParams rejects unknown fields, so the provider must declare it.
func TestObserveAcceptsTheSecretsFieldBlockadeSends(t *testing.T) {
	p := &provider{descriptor: plugin.Descriptor{Identity: plugin.Identity{ID: "onnx"}}}
	params := `{"request":{"apiVersion":"content.adapter/v1","request":{"apiVersion":"content.observation/v1","requestId":"r1","image":""}},"secrets":{}}`
	_, err := p.call(context.Background(), "content.provider.observe", json.RawMessage(params))
	// The provider has not been started, which is the error to see here; an
	// "unknown field" error would mean the secrets field was rejected.
	if err == nil || strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("observe with secrets = %v", err)
	}
}
