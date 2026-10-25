package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	buildGuest(t, executable, "pkg/console/cmd/content")
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

	// Upgrade through the public command, then execute the newly discovered
	// package. A rejected upgrade must leave the previous package usable.
	// ext revisions are content digests, not the display version. Produce a
	// distinct valid binary instead of merely changing the version label.
	upgradeBuild := exec.Command("go", "build", "-ldflags=-buildid=published-ext-upgrade", "-o", executable, "./cmd/content")
	upgradeBuild.Dir = filepath.Join("..", "pkg", "console")
	if output, err := upgradeBuild.CombinedOutput(); err != nil {
		t.Fatalf("build upgrade: %v\n%s", err, output)
	}
	manifest, err = packagekit.ExecutableManifest("content", "1.0.1", []plugin.Contract{pluginContract(contentServiceContract)}, executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteManifest(source, manifest); err != nil {
		t.Fatal(err)
	}
	if err := pluginsCommand([]string{"upgrade", "--manifest", manifestPath}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := pluginsCommand([]string{"upgrade", "--manifest", manifestPath}, io.Discard, io.Discard); err == nil {
		t.Fatal("same revision upgrade succeeded")
	}
	output.Reset()
	if err := runContent([]string{"validate", "--config", configPath}, &output, &errors); err != nil || output.String() != "valid content config: 1 adapter(s)\n" {
		t.Fatalf("validate after upgrade = %q, %v", output.String(), err)
	}
	installed, err := contentServicePackage()
	if err != nil {
		t.Fatal(err)
	}
	if installed.Manifest.Descriptor.Identity.Revision != manifest.Descriptor.Identity.Revision {
		t.Fatalf("upgrade revision = %s", installed.Manifest.Descriptor.Identity.Revision)
	}
	program, err := installed.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(program, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(program, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := runContent([]string{"validate", "--config", configPath}, &output, &errors); err == nil {
		t.Fatal("tampered content service executed")
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

// lockedBuffer collects streamed log lines that arrive on another goroutine.
type lockedBuffer struct {
	mu   sync.Mutex
	text strings.Builder
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.String()
}

func TestContentServeStreamsItsLogsAndStopsWhenCanceled(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(root, "installed"))
	t.Setenv("CYMONKEY_CONTENT_CONFIG", "")
	build := func(program, name string) string {
		out := filepath.Join(root, name)
		if program == "tests/plugin-fixture" {
			// The fixture belongs to the host's module.
			cmd := exec.Command("go", "build", "-o", out, "./"+program)
			cmd.Dir = filepath.Dir(filepath.Dir(fixtureSourcePath(t)))
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("build %s: %v\n%s", program, err, output)
			}
			return out
		}
		buildGuest(t, out, program)
		return out
	}
	install := func(name, program, contract string) {
		source := filepath.Join(root, "source-"+name)
		if err := os.Mkdir(source, 0o700); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(program)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "provider"), data, 0o700); err != nil {
			t.Fatal(err)
		}
		manifest, err := packagekit.ExecutableManifest(name, "1.0.0", []plugin.Contract{pluginContract(contract)}, filepath.Join(source, "provider"))
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
	}
	install("content", build("pkg/console/cmd/content", "content"), contentServiceContract)
	install("vision-fixture", build("tests/plugin-fixture", "fixture"), contentPluginContract)

	configPath := filepath.Join(root, "content.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: cymonkey.content-config/v1alpha1\nadapters:\n  - id: vision\n    plugin: vision-fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()

	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithCancel(context.Background())
		if attempt == 1 {
			cancel()
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		}
		defer cancel()
		var stdout bytes.Buffer
		var logs lockedBuffer
		done := make(chan error, 1)
		go func() {
			done <- runContentContext(ctx, []string{"serve", "--config", configPath, "--bind", address}, &stdout, &logs)
		}()

		// The log line the engine prints once it listens reaches the host while the
		// command is still running.
		deadline := time.Now().Add(20 * time.Second)
		for !strings.Contains(logs.String(), "listening on") {
			if time.Now().After(deadline) {
				t.Fatalf("no streamed log line; logs = %q", logs.String())
			}
			time.Sleep(25 * time.Millisecond)
		}
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://" + address + "/healthz")
		if err != nil {
			t.Fatalf("the served content engine does not answer: %v", err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("healthz status = %d", response.StatusCode)
		}

		response, err = client.Post("http://"+address+"/v1/observe", "application/json", strings.NewReader(`{"requestId":"published-ext","image":"aW1hZ2U="}`))
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK || !bytes.Contains(data, []byte(`"requestId":"published-ext"`)) {
			t.Fatalf("observation = %d %s %v", response.StatusCode, data, readErr)
		}
		if attempt == 0 {
			cancel()
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("serve returned %v after cancellation", err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("serve did not stop when canceled")
		}
		if ctx.Err() == nil {
			t.Fatal("serve returned before cancellation/deadline")
		}
		// Reclaim the exact listening address immediately, then start the service
		// again on that address in the next iteration.
		reclaimed, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("service retained its listener after close: %v", err)
		}
		if err := reclaimed.Close(); err != nil {
			t.Fatal(err)
		}
		t.Logf("cycle %d: observation, streamed readiness, %v, listener reclaimed", attempt+1, ctx.Err())
	}
}
