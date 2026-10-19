package consolecmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testConfig = `{
  "apiVersion": "cymonkey.console/v1alpha1",
  "models": [{"id": "local", "provider": "local-llm", "model": "m1", "protocol": "openai-compatible", "baseURL": "http://127.0.0.1:1", "capabilities": ["text.generate"]}],
  "tools": [{"definition": {"name": "ask", "description": "Ask the model", "inputSchema": {"type": "object"}}, "kind": "generate", "modelId": "local"}]
}`

func writeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "console.json")
	if err := os.WriteFile(path, []byte(testConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func run(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), args, strings.NewReader(stdin), &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func TestToolsAndModelsListTheConfiguredInventory(t *testing.T) {
	config := writeConfig(t)
	tools, _, err := run(t, "", "tools", "--config", config)
	if err != nil || !strings.Contains(tools, `"ask"`) {
		t.Fatalf("tools = %q, %v", tools, err)
	}
	models, _, err := run(t, "", "models", "--config", config)
	if err != nil || !strings.Contains(models, `"local"`) {
		t.Fatalf("models = %q, %v", models, err)
	}
}

func TestACallReadsItsRequestFromTheGivenInput(t *testing.T) {
	config := writeConfig(t)
	_, _, err := run(t, `{"name":"nope","arguments":{}}`, "call", "--config", config)
	if err == nil || !strings.Contains(err.Error(), "nope") && !strings.Contains(strings.ToLower(err.Error()), "unknown") && !strings.Contains(strings.ToLower(err.Error()), "not") {
		t.Fatalf("a call to an unregistered tool: %v", err)
	}
	if _, _, err := run(t, `{"name":`, "call", "--config", config); err == nil {
		t.Fatal("a malformed request was accepted")
	}
}

func TestCommandsRefuseMissingConfigurationAndUnknownCommandsAndOpenListeners(t *testing.T) {
	if _, _, err := run(t, "", "tools"); err == nil {
		t.Fatal("a command ran without --config")
	}
	if _, _, err := run(t, "", "teleport", "--config", "x"); err == nil {
		t.Fatal("an unknown command ran")
	}
	config := writeConfig(t)
	t.Setenv("TEST_CONSOLE_TOKEN", "secret")
	if _, _, err := run(t, "", "serve", "--config", config, "--bind", "0.0.0.0:0", "--token-env", "TEST_CONSOLE_TOKEN"); err == nil {
		t.Fatal("serve accepted a non-loopback address")
	}
}
