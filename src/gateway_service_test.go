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
	"testing"
	"time"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
)

func TestDisplayGatewayServesAsAnInstalledPackageAndStopsWhenCanceled(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(root, "installed"))
	t.Setenv("GATEWAY_TEST_TOKEN", "gateway-secret")

	var none bytes.Buffer
	if err := gatewayCommandContext(context.Background(), []string{"serve"}, &none, &none); err == nil || !strings.Contains(err.Error(), "no display gateway is installed") {
		t.Fatalf("without a gateway: %v", err)
	}

	source := filepath.Join(root, "gateway")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(source, "gateway")
	build := exec.Command("go", "build", "-o", executable, "board/cmd/gateway")
	build.Dir = filepath.Dir(filepath.Dir(fixtureSourcePath(t)))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gateway: %v\n%s", err, output)
	}
	manifest, err := packagekit.ExecutableManifest("gateway", "1.0.0", []plugin.Contract{pluginContract(gatewayServiceContract)}, executable)
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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var logs lockedBuffer
	var stdout bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- gatewayCommandContext(ctx, []string{"serve", "--bind", address, "--upstream", "127.0.0.1:1", "--token-env", "GATEWAY_TEST_TOKEN"}, &stdout, &logs)
	}()
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(logs.String(), "listening on") {
		if time.Now().After(deadline) {
			t.Fatalf("no streamed log line; logs = %q", logs.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
	// The gateway answers and refuses a request that lacks the token.
	response, err := http.Get("http://" + address + "/")
	if err != nil {
		t.Fatalf("the gateway does not answer: %v", err)
	}
	response.Body.Close()
	if response.StatusCode/100 == 2 {
		t.Fatalf("an unauthenticated request was served: %d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("gateway returned %v after cancellation", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the gateway did not stop when canceled")
	}
}

func TestGatewayLaunchEnvironmentPassesTheNamedTokenOnly(t *testing.T) {
	t.Setenv("GATEWAY_TEST_TOKEN", "t")
	t.Setenv("CYMONKEY_DEVICE_GATEWAY_TOKEN", "default")
	t.Setenv("UNRELATED_SECRET", "no")
	environment := strings.Join(gatewayLaunchEnvironment([]string{"serve", "--token-env", "GATEWAY_TEST_TOKEN"}), "\n")
	for _, want := range []string{"GATEWAY_TEST_TOKEN=t", "CYMONKEY_DEVICE_GATEWAY_TOKEN=default"} {
		if !strings.Contains(environment, want) {
			t.Errorf("environment lacks %q:\n%s", want, environment)
		}
	}
	if strings.Contains(environment, "UNRELATED_SECRET") {
		t.Errorf("an unnamed variable was passed:\n%s", environment)
	}
}
