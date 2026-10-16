package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"blockade"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
)

func TestModelInspectionRejectsWrongArtifactWithoutStarting(t *testing.T) {
	p := &provider{descriptor: plugin.Descriptor{Identity: plugin.Identity{ID: "onnx"}}}
	model := blockade.InstalledModel{Manifest: blockade.ModelManifest{
		APIVersion: blockade.ModelManifestAPIVersion,
		Name:       "detector", Version: "1.0.0", Provider: "onnx",
		Capabilities: []string{blockade.CapabilityObjectDetect},
		Artifacts:    []blockade.ModelArtifact{{Role: "yolo", File: "detector.gguf", SHA256: strings.Repeat("0", 64)}},
	}, Directory: t.TempDir()}
	params, err := json.Marshal(map[string]any{"model": model})
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.call(context.Background(), "blockade.model.inspect", params)
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
	if _, err := p.call(context.Background(), "blockade.start", params); err == nil {
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
