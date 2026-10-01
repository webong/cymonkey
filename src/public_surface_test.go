package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"cymonkey/src/contracts"
)

func TestPublicCommandsHideInternalLibraries(t *testing.T) {
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(t.TempDir(), "plugins"))
	for _, command := range []string{"board", "blockade", "provider"} {
		if err := run([]string{command, "help"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("internal command %q is public", command)
		}
	}
	for _, command := range []string{"device", "content", "display", "modules", "plugins"} {
		var stdout, stderr bytes.Buffer
		if err := run([]string{command, "help"}, &stdout, &stderr); err != nil {
			t.Fatalf("%s help: %v", command, err)
		}
		text := strings.ToLower(stdout.String() + stderr.String())
		if internal := regexp.MustCompile(`\b(board|blockade|jangolova)\b`).FindString(text); internal != "" {
			t.Fatalf("%s help exposes %q: %s", command, internal, text)
		}
	}
	var rootHelp bytes.Buffer
	if err := run([]string{"help"}, io.Discard, &rootHelp); err != nil {
		t.Fatal(err)
	}
	if internal := regexp.MustCompile(`\b(board|blockade|jangolova)\b`).FindString(strings.ToLower(rootHelp.String())); internal != "" {
		t.Fatalf("root help exposes %q: %s", internal, rootHelp.String())
	}
}

func TestContentCommandPersistsCymonkeyConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(root, "plugins"))
	configPath := filepath.Join(root, "content.yaml")
	var output bytes.Buffer
	if err := run([]string{
		"content", "endpoint", "add",
		"--config", configPath,
		"--id", "local-chat",
		"--profile", "ollama",
		"--url", "http://127.0.0.1:11434",
		"--model", "llama3",
	}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"apiVersion: cymonkey.content-config/v1alpha1", "adapters:", "plugin: openai-compatible"} {
		if !strings.Contains(text, want) {
			t.Fatalf("public content config lacks %q:\n%s", want, text)
		}
	}
	for _, internal := range []string{"blockade", "providers:", "provider:"} {
		if strings.Contains(strings.ToLower(text), internal) {
			t.Fatalf("public content config exposes %q:\n%s", internal, text)
		}
	}

	output.Reset()
	if err := run([]string{"content", "endpoint", "list", "--config", configPath}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "provider") || !strings.Contains(output.String(), `"plugin":"openai-compatible"`) {
		t.Fatalf("public endpoint inventory = %s", output.String())
	}
	output.Reset()
	if err := run([]string{"content", "validate", "--config", configPath}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "valid content config: 1 adapter(s)\n" {
		t.Fatalf("validation output = %q", output.String())
	}
}

func TestDeviceJSONUsesCymonkeyContract(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"device", "devices", "--root", "photos=" + t.TempDir(), "--json"}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "providerId") || !strings.Contains(output.String(), `"apiVersion":"cymonkey.device/v1alpha1"`) || !strings.Contains(output.String(), `"adapterId":`) {
		t.Fatalf("public device inventory = %s", output.String())
	}
}

func TestContentRuntimeManifestUsesCymonkeyContract(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(root, "plugins"))
	t.Setenv("CYMONKEY_RUNTIME_DIR", filepath.Join(root, "installed-runtimes"))
	source := filepath.Join(root, "runtime")
	if err := os.MkdirAll(filepath.Join(source, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "bin", "python3"), []byte("runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(source, "runtime.json")
	var output bytes.Buffer
	if err := run([]string{"content", "runtime", "manifest", "--source", source, "--name", "python", "--version", "3.12.8", "--executable", "bin/python3", "--output", manifest}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "blockade") || !strings.Contains(string(data), contracts.RuntimeManifestVersion) {
		t.Fatalf("public runtime manifest = %s", data)
	}
	output.Reset()
	if err := run([]string{"content", "runtime", "install", "--manifest", manifest}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(output.String()), "blockade") || !strings.Contains(output.String(), contracts.RuntimeManifestVersion) {
		t.Fatalf("public runtime install output = %s", output.String())
	}
}

func TestContentModelManifestIsTranslatedOnlyAtBoundary(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "model.json")
	if err := writePublicJSON(manifest, map[string]any{
		"apiVersion": contracts.ModelManifestVersion,
		"name":       "sample", "version": "1.0.0", "plugin": "onnx",
		"capabilities": []string{"image.observe"},
		"artifacts":    []any{map[string]any{"role": "weights", "file": "model.onnx", "sha256": strings.Repeat("0", 64)}},
	}); err != nil {
		t.Fatal(err)
	}
	args, cleanup, _, err := prepareContentArtifacts([]string{"model", "install", "--manifest", manifest, "--adapter", "vision"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	translated, ok := contentFlagValue(args, "manifest")
	if !ok || translated == manifest {
		t.Fatalf("model manifest was not translated: %#v", args)
	}
	data, err := os.ReadFile(translated)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"apiVersion": "blockade.model/v1alpha1"`) || !strings.Contains(string(data), `"provider": "onnx"`) {
		t.Fatalf("library model manifest = %s", data)
	}
	publicData, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(publicData), "blockade") || !strings.Contains(string(publicData), `"plugin": "onnx"`) {
		t.Fatalf("public model manifest changed = %s", publicData)
	}
}

func TestContentServiceTranslatesPublicHTTPContract(t *testing.T) {
	internal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["apiVersion"] != "blockade.content/v1alpha1" {
			t.Fatalf("library request = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"apiVersion": "blockade.content/v1alpha1",
			"findings":   []any{map[string]any{"kind": "summary", "text": "done", "sourceParts": []int{0}}},
		})
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/interpret", strings.NewReader(`{"apiVersion":"cymonkey.content/v1alpha1","parts":[{"kind":"text","text":"hello"}]}`))
	response := httptest.NewRecorder()
	contentServiceContract(internal).ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "blockade") || !strings.Contains(response.Body.String(), contracts.ContentAPIVersion) {
		t.Fatalf("public content response = %d %s", response.Code, response.Body.String())
	}
}
