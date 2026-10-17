package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
)

func TestContentCommandsRunInTheInstalledContentService(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(root, "installed"))
	t.Setenv("CYMONKEY_CONTENT_CONFIG", "")

	var none bytes.Buffer
	if err := runContent([]string{"validate"}, &none, &none); err == nil || !strings.Contains(err.Error(), "no content service is installed") {
		t.Fatalf("without a service: %v", err)
	}

	source := filepath.Join(root, "content")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(source, "content")
	build := exec.Command("go", "build", "-o", executable, "console/cmd/content")
	build.Dir = filepath.Dir(filepath.Dir(fixtureSourcePath(t)))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build content service: %v\n%s", err, output)
	}
	manifest, err := packagekit.ExecutableManifest("content", "1.0.0", []plugin.Contract{pluginContract(contentServiceContract)}, executable)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(source, store.ManifestName)
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pluginsCommand([]string{"install", "--manifest", manifestPath}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	// The commands run inside the package: its output and errors come back.
	configPath := filepath.Join(root, "content.yaml")
	var output, errors bytes.Buffer
	if err := runContent([]string{"endpoint", "add", "--config", configPath, "--id", "local-chat", "--profile", "ollama", "--url", "http://127.0.0.1:11434", "--model", "llama3"}, &output, &errors); err != nil {
		t.Fatalf("endpoint add: %v\n%s", err, errors.String())
	}
	output.Reset()
	if err := runContent([]string{"validate", "--config", configPath}, &output, &errors); err != nil || output.String() != "valid content config: 1 adapter(s)\n" {
		t.Fatalf("validate = %q, %v", output.String(), err)
	}
	if err := runContent([]string{"validate", "--config", filepath.Join(root, "missing.yaml")}, &output, &errors); err == nil {
		t.Fatal("a missing config was reported valid")
	}

	// The host admits only the operations the content service serves.
	if err := contentServicePolicy(context.Background(), plugin.Request{Operation: "reboot"}); err == nil {
		t.Fatal("an unknown content service operation was admitted")
	}
}

func TestContentLaunchEnvironmentPassesOnlyNamedVariables(t *testing.T) {
	t.Setenv("HOME", "/home/test")
	t.Setenv("UNRELATED_SECRET", "no")
	t.Setenv("PROVIDER_KEY", "yes")
	t.Setenv("SERVICE_TOKEN", "token")
	config := filepath.Join(t.TempDir(), "content.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: cymonkey.content-config/v1alpha1\nadapters:\n  - id: hosted\n    plugin: openai-compatible\n    secrets:\n      apiKey:\n        env: PROVIDER_KEY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := strings.Join(contentLaunchEnvironment([]string{"serve", "--config", config, "--token-env", "SERVICE_TOKEN"}), "\n")
	for _, want := range []string{"HOME=/home/test", "PROVIDER_KEY=yes", "SERVICE_TOKEN=token"} {
		if !strings.Contains(environment, want) {
			t.Errorf("environment lacks %q:\n%s", want, environment)
		}
	}
	if strings.Contains(environment, "UNRELATED_SECRET") {
		t.Errorf("an unnamed variable was passed:\n%s", environment)
	}
}
