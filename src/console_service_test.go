package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	commandrun "jangolova/commandkit/run"
)

const consoleTestConfig = `{
  "apiVersion": "cymonkey.console/v1alpha1",
  "models": [{"id": "local", "provider": "local-llm", "model": "m1", "protocol": "openai-compatible", "baseURL": "http://127.0.0.1:1", "apiKeyEnvironment": "CONSOLE_TEST_KEY", "capabilities": ["text.generate"]}],
  "tools": [{"definition": {"name": "ask", "description": "Ask the model", "inputSchema": {"type": "object"}}, "kind": "generate", "modelId": "local"}]
}`

func installConsoleService(t *testing.T) store.Installed {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(root, "installed"))
	source := filepath.Join(root, "console")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(source, "console")
	buildGuest(t, executable, "pkg/console/cmd/console")
	manifest, err := packagekit.ExecutableManifest("console", "1.0.0", []plugin.Contract{pluginContract(consoleServiceContract)}, executable)
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
	installed, err := consoleServicePackage()
	if err != nil {
		t.Fatal(err)
	}
	return installed
}

func TestConsoleCommandsRunInTheInstalledConsoleService(t *testing.T) {
	var none bytes.Buffer
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(t.TempDir(), "installed"))
	if err := consoleCommand([]string{"tools", "--config", "x"}, &none, &none); err == nil || !strings.Contains(err.Error(), "no console service is installed") {
		t.Fatalf("without a service: %v", err)
	}
	installConsoleService(t)

	// A relative configuration path means what the user typed: the command runs
	// in the directory it was started from, not in the package.
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "console.json"), []byte(consoleTestConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, _ := os.Getwd()
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)

	var output, logs bytes.Buffer
	if err := consoleCommand([]string{"tools", "--config", "console.json"}, &output, &logs); err != nil || !strings.Contains(output.String(), `"ask"`) {
		t.Fatalf("tools = %q, %v\n%s", output.String(), err, logs.String())
	}
	output.Reset()
	if err := consoleCommand([]string{"models", "--config", "console.json"}, &output, &logs); err != nil || !strings.Contains(output.String(), `"local"`) {
		t.Fatalf("models = %q, %v", output.String(), err)
	}
	if err := consoleCommand([]string{"models"}, &output, &logs); err == nil {
		t.Fatal("a command without --config succeeded")
	}
}

func TestConsoleStdioCommandsRunAsAChildWithTheCallersInputAndOutput(t *testing.T) {
	installed := installConsoleService(t)
	config := filepath.Join(t.TempDir(), "console.json")
	if err := os.WriteFile(config, []byte(consoleTestConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	// The stream command reads its request from standard input; a request for a
	// model that is not configured fails inside the console, which shows the
	// input reached it.
	var stdout, stderr bytes.Buffer
	err := commandrun.Exec(context.Background(), installed, []string{"stream", "--config", config}, nil,
		strings.NewReader(`{"model":"missing","messages":[{"role":"user","content":"hi"}]}`), &stdout, &stderr)
	if err == nil {
		t.Fatalf("a stream for an unknown model succeeded: %q", stdout.String())
	}
}

func TestConsoleInputIsReadOnlyForCallAndGenerateWithoutAnInputFile(t *testing.T) {
	for _, command := range [][]string{{"tools"}, {"models"}, {"serve"}, {"call", "--input", "request.json"}, {"generate", "--input=request.json"}} {
		data, err := consoleInput(command, strings.NewReader("ignored"))
		if err != nil || data != nil {
			t.Errorf("%v read input %q, %v", command, data, err)
		}
	}
	data, err := consoleInput([]string{"call", "--config", "c"}, strings.NewReader(`{"name":"x"}`))
	if err != nil || string(data) != `{"name":"x"}` {
		t.Fatalf("call input = %q, %v", data, err)
	}
	if _, err := consoleInput([]string{"generate"}, strings.NewReader(strings.Repeat("a", 1<<20+1))); err == nil {
		t.Fatal("an oversized request was read")
	}
}

func TestConsoleLaunchEnvironmentPassesOnlyNamedVariables(t *testing.T) {
	t.Setenv("HOME", "/home/test")
	t.Setenv("UNRELATED_SECRET", "no")
	t.Setenv("CONSOLE_TEST_KEY", "key")
	t.Setenv("CONSOLE_TEST_TOKEN", "token")
	config := filepath.Join(t.TempDir(), "console.json")
	if err := os.WriteFile(config, []byte(consoleTestConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := strings.Join(consoleLaunchEnvironment([]string{"serve", "--config", config, "--token-env", "CONSOLE_TEST_TOKEN"}), "\n")
	for _, want := range []string{"HOME=/home/test", "CONSOLE_TEST_KEY=key", "CONSOLE_TEST_TOKEN=token"} {
		if !strings.Contains(environment, want) {
			t.Errorf("environment lacks %q:\n%s", want, environment)
		}
	}
	if strings.Contains(environment, "UNRELATED_SECRET") {
		t.Errorf("an unnamed variable was passed:\n%s", environment)
	}
}
