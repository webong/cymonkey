package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	if err := run([]string{"extension", "act", "--name", "extension.install", "--input", `{"source":"/somewhere","target":{"browser":"chrome"}}`}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("accepted tool install without a reviewed revision")
	}
	installRequest, err := json.Marshal(map[string]any{
		"source": source, "revision": prepared.Revision,
		"destination": filepath.Join(t.TempDir(), "native-staged"),
		"target": map[string]string{
			"browser": "chrome", "executablePath": filepath.Join(t.TempDir(), "missing-browser"),
			"userDataDir": filepath.Join(t.TempDir(), "user-data"), "profileDirectory": "Default",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"extension", "act", "--name", "extension.install", "--input", string(installRequest)}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !os.IsNotExist(err) {
		t.Fatalf("structured install did not reach the selected browser: %v", err)
	}
	firefoxRequest, err := json.Marshal(map[string]any{
		"source": filepath.Join(t.TempDir(), "signed.xpi"), "revision": prepared.Revision,
		"target": map[string]string{"browser": "firefox", "executablePath": filepath.Join(t.TempDir(), "missing-firefox"), "profilePath": filepath.Join(t.TempDir(), "firefox-profile")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"extension", "act", "--name", "extension.install", "--input", string(firefoxRequest)}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("Firefox install accepted a nonexistent XPI")
	}
	if err := run([]string{"extension", "act", "--name", "extension.install", "--input", `{"source":"/somewhere","revision":"sha256:example","target":{"browser":"safari"}}`}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("accepted an unsupported browser through the long-running action")
	}
	capability, err := extensionAction("extension.capabilities", []byte(`{"target":{"browser":"safari"}}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(capability)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"installDriver":"signed-containing-app"`)) || !bytes.Contains(encoded, []byte(`"requiresBrowserAction":true`)) {
		t.Fatalf("incorrect Safari target capability: %s", encoded)
	}
	capability, err = extensionAction("extension.capabilities", []byte(`{"target":{"browser":"chrome"}}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(capability)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"requiresBrowserAction":true`)) {
		t.Fatalf("native installation action was not disclosed: %s", encoded)
	}
}

func TestExplicitAndDiscoveredTargetSelection(t *testing.T) {
	selected, err := resolveCLITarget("", "firefox", "/browser/firefox", "/profiles/personal", "")
	if err != nil || selected.Browser != "firefox" || selected.ProfilePath != "/profiles/personal" {
		t.Fatalf("explicit Firefox target was lost: %+v, %v", selected, err)
	}
	if _, err := resolveCLITarget("not-a-target", "firefox", "", "", ""); err == nil {
		t.Fatal("accepted both target ID and explicit browser")
	}
	if _, err := resolveCLITarget("not-a-target", "", "", "", ""); err == nil {
		t.Fatal("accepted a stale target ID")
	}
	if _, err := parseExtensionActionInput([]byte(`{"target":{"id":"one","browser":"firefox"}}`)); err == nil {
		t.Fatal("accepted conflicting structured target")
	}
	var output bytes.Buffer
	if err := run([]string{"browser", "targets"}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var targets []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(output.Bytes(), &targets); err != nil {
		t.Fatalf("browser targets did not return JSON: %v", err)
	}
}

func TestDefaultChromeProfileUsesManualInstallHandoff(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS Chrome default profile")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "extension")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "manifest.json"), []byte(`{"manifest_version":3,"name":"Test","version":"1.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	prepared, err := inspectExtensionSource(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	var output bytes.Buffer
	err = run([]string{"extension", "install", "--browser", "chrome", "--profile", target, "--source", source,
		"--revision", prepared.Revision, "--destination", filepath.Join(t.TempDir(), "staged")}, &output, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ Status, Profile string }
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "awaiting-browser-action" || result.Profile != filepath.Join(target, "Default") {
		t.Fatalf("incorrect manual Chrome handoff: %+v", result)
	}
}
