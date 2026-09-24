package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExtensionCLIInspectAndInstallHandoff(t *testing.T) {
	source := filepath.Join(t.TempDir(), "extension")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "manifest.json"), []byte(`{"manifest_version":3,"name":"Test Extension","version":"1.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"extension", "prepare", "--source", source}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var inspected struct {
		Name     string `json:"name"`
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(output.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.Name != "Test Extension" || inspected.Revision == "" {
		t.Fatalf("unexpected inspection: %+v", inspected)
	}
	output.Reset()
	destination := filepath.Join(t.TempDir(), "staged")
	if err := run([]string{"extension", "stage", "--browser", "chrome", "--source", source, "--revision", inspected.Revision, "--destination", destination}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Status string `json:"status"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "awaiting-browser-action" || result.Source != destination {
		t.Fatalf("unexpected install result: %+v", result)
	}
	if err := run([]string{"extension", "stage", "--browser", "chrome", "--source", source, "--revision", "sha256:wrong", "--destination", filepath.Join(t.TempDir(), "rejected")}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("accepted a revision that differs from the inspected source")
	}
}

func TestExtensionActionForOtherLocalTools(t *testing.T) {
	source := filepath.Join(t.TempDir(), "extension")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "manifest.json"), []byte(`{"manifest_version":3,"name":"External Tool Extension","version":"1.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	prepareInput, err := json.Marshal(map[string]string{"source": source})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"extension", "act", "--name", "extension.prepare", "--input", string(prepareInput)}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var prepared struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(output.Bytes(), &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Revision == "" {
		t.Fatal("prepare omitted revision")
	}
	installInput, err := json.Marshal(map[string]any{"source": source, "revision": prepared.Revision, "destination": filepath.Join(t.TempDir(), "staged"), "target": map[string]string{"browser": "chrome"}})
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run([]string{"extension", "act", "--name", "extension.stage", "--input", string(installInput)}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var installed struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(output.Bytes(), &installed); err != nil {
		t.Fatal(err)
	}
	if installed.Status != "awaiting-browser-action" {
		t.Fatalf("incorrect install state: %+v", installed)
	}
	if _, err := extensionAction("extension.install", []byte(`{"source":"/somewhere"}`)); err == nil {
		t.Fatal("accepted tool install without a reviewed revision")
	}
	if _, err := extensionAction("extension.install", []byte(`{"source":"/somewhere","revision":"sha256:example","target":{"browser":"safari"}}`)); err == nil {
		t.Fatal("claimed direct installation support for an unsupported browser")
	}
	capability, err := extensionAction("extension.capabilities", []byte(`{"target":{"browser":"safari"}}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(capability)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"persistentLocalInstall":false`)) || !bytes.Contains(encoded, []byte(`"sessionLoad":false`)) {
		t.Fatalf("incorrect unsupported target capability: %s", encoded)
	}
}
