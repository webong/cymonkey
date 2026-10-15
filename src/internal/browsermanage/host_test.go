package browsermanage

import (
	"context"
	"encoding/json"
	"github.com/webong/ext/res/web/contract"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A fake browser adapter that speaks the management protocol. The first
// argument after the profile marker selects behavior through the action name.
const fakeAdapter = `#!/bin/sh
# usage: share PROFILE -- management KIND ACTION
profile="$2"; kind="$5"; action="$6"
cat >/dev/null
case "$action" in
  prepare)
    printf '{"version":"1.0","kind":"%s","action":"%s","status":"prepared","result":{"profile":"%s"}}\n' "$kind" "$action" "$profile" ;;
  activate)
    echo 'browser management progress: {"status":"loading"}' >&2
    echo 'plain diagnostic' >&2
    printf '{"version":"1.0","kind":"%s","action":"%s","status":"activated"}\n' "$kind" "$action" ;;
  install)
    printf '{"version":"1.0","kind":"%s","action":"%s","status":"activated"}\n' "$kind" "$action" ;;
  package)
    printf '{"version":"1.0","kind":"%s","action":"stage","status":"prepared"}\n' "$kind" ;;
  stage)
    echo 'adapter exploded' >&2; exit 1 ;;
  capabilities)
    sleep 30 ;;
esac
`

const fakeManifest = `api_version = "2.0"
name = "fakebrowser"
display_name = "Fake"
runtime = "browser"
surfaces = "web"
executable = "ctx-fake"
description = "test adapter"
capabilities = "list,validate,open,doctor,share"
selector_key = "browser"
share_spaces = "browser"
browser_share = "cookie.list"
browser_management = "extension.prepare,extension.activate,extension.install,extension.package,extension.stage,extension.capabilities"
`

func newHost(t *testing.T, trust bool) *Host {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake adapter is a POSIX script")
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "adapter.toml"), []byte(fakeManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "ctx-fake"), []byte(fakeAdapter), 0o700); err != nil {
		t.Fatal(err)
	}
	host := New(t.TempDir())
	installed, err := host.Store().Install(source)
	if err != nil {
		t.Fatal(err)
	}
	if trust {
		if err := host.Store().Trust(installed); err != nil {
			t.Fatal(err)
		}
	}
	return host
}

func TestDoRunsATrustedAdapterAndReportsProgress(t *testing.T) {
	host := newHost(t, true)
	endpoint, err := host.Endpoint("fakebrowser:Profile 1")
	if err != nil {
		t.Fatal(err)
	}
	response, err := host.Do(context.Background(), endpoint, "extension", "prepare", map[string]string{"source": "/x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ Profile string }
	if err := Result(response, &result); err != nil || response.Status != "prepared" || result.Profile != "Profile 1" {
		t.Fatalf("response = %#v %#v %v", response, result, err)
	}
	if Installed(response) {
		t.Fatal("a prepared package counted as installed")
	}
	var progress []string
	response, err = host.Do(context.Background(), endpoint, "extension", "activate", map[string]string{"source": "/x"}, func(raw json.RawMessage) error {
		progress = append(progress, string(raw))
		return nil
	})
	if err != nil || response.Status != "activated" || len(progress) != 1 || progress[0] != `{"status":"loading"}` {
		t.Fatalf("activate = %#v %v progress %v", response, err, progress)
	}
	if Installed(response) {
		t.Fatal("a session-only activation counted as installed")
	}
}

func TestDoRefusesUntrustedAdaptersAndUndeclaredOperations(t *testing.T) {
	untrusted := newHost(t, false)
	endpoint, err := untrusted.Endpoint("fakebrowser")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := untrusted.Do(context.Background(), endpoint, "extension", "prepare", nil, nil); err == nil {
		t.Fatal("an untrusted adapter ran")
	}
	trusted := newHost(t, true)
	endpoint, _ = trusted.Endpoint("fakebrowser")
	if _, err := trusted.Do(context.Background(), endpoint, "extension", "sign", nil, nil); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("undeclared operation error = %v", err)
	}
	if _, err := trusted.Do(context.Background(), endpoint, "extension", "nonsense", nil, nil); err == nil {
		t.Fatal("an operation outside the protocol catalog ran")
	}
}

func TestDoRejectsMismatchedResponsesAndInstallBoundary(t *testing.T) {
	host := newHost(t, true)
	endpoint, _ := host.Endpoint("fakebrowser")
	if _, err := host.Do(context.Background(), endpoint, "extension", "package", nil, nil); err == nil {
		t.Fatal("a response for another action was accepted")
	}
	if _, err := host.Do(context.Background(), endpoint, "extension", "install", nil, nil); err == nil || !strings.Contains(err.Error(), "session activation") {
		t.Fatalf("install reporting activation error = %v", err)
	}
}

func TestDoSurfacesAdapterFailuresAndCancellation(t *testing.T) {
	host := newHost(t, true)
	endpoint, _ := host.Endpoint("fakebrowser")
	if _, err := host.Do(context.Background(), endpoint, "extension", "stage", nil, nil); err == nil || !strings.Contains(err.Error(), "adapter exploded") {
		t.Fatalf("failing adapter error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := host.Do(ctx, endpoint, "extension", "capabilities", nil, nil); err == nil {
		t.Fatal("a cancelled call succeeded")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("cancellation did not stop the adapter promptly")
	}
}

func TestAdaptersListsOnlyDeclaredOperations(t *testing.T) {
	host := newHost(t, true)
	found, err := host.Adapters("extension.prepare")
	if err != nil || len(found) != 1 {
		t.Fatalf("adapters = %v %v", found, err)
	}
	if found, _ = host.Adapters("extension.sign"); len(found) != 0 {
		t.Fatalf("adapters for an undeclared operation = %v", found)
	}
	if _, err := host.Endpoint(""); err == nil {
		t.Fatal("an empty selection resolved")
	}
}

const cookieAdapter = `#!/bin/sh
# usage: share PROFILE -- cookie OPERATION
cat > "$FAKE_ADAPTER_LOG/request.json"
case "$5" in
  query) printf '{"cookies":[{"name":"session","value":"synthetic-secret","domain":"example.com","path":"/","expiry":0,"secure":true,"http_only":true}],"store_id":"0"}\n' ;;
  normalize) echo 'could not read the store: synthetic-secret' >&2; exit 2 ;;
esac
`

func TestCookieQueryRunsAShareOperationAndNeverLeaksValuesInErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake adapter is a POSIX script")
	}
	t.Setenv("FAKE_ADAPTER_LOG", t.TempDir())
	source := t.TempDir()
	manifest := strings.Replace(fakeManifest, `browser_share = "cookie.list"`, `browser_share = "cookie.query,cookie.normalize"`, 1)
	if err := os.WriteFile(filepath.Join(source, "adapter.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "ctx-fake"), []byte(cookieAdapter), 0o700); err != nil {
		t.Fatal(err)
	}
	host := New(t.TempDir())
	installed, err := host.Store().Install(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Store().Trust(installed); err != nil {
		t.Fatal(err)
	}
	endpoint, err := host.Endpoint("fakebrowser:Default")
	if err != nil {
		t.Fatal(err)
	}
	result, err := host.Cookie(context.Background(), endpoint, "query", contract.CookieRequest{Site: "https://example.com/", Names: []string{"session"}})
	if err != nil || len(result.Cookies) != 1 || result.Cookies[0].Value != "synthetic-secret" || result.StoreID != "0" {
		t.Fatalf("query = %#v %v", result, err)
	}
	sent, err := os.ReadFile(filepath.Join(os.Getenv("FAKE_ADAPTER_LOG"), "request.json"))
	if err != nil || !strings.Contains(string(sent), `"version":2`) || !strings.Contains(string(sent), `"site":"https://example.com/"`) {
		t.Fatalf("adapter request = %s %v", sent, err)
	}
	if _, err := host.Cookie(context.Background(), endpoint, "export", contract.CookieRequest{}); err == nil {
		t.Fatal("an unsupported cookie operation ran")
	}
	_, err = host.Cookie(context.Background(), endpoint, "normalize", contract.CookieRequest{StoreID: "0"})
	if err == nil {
		t.Fatal("a failing adapter reported success")
	}
	if strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatalf("a cookie failure echoed adapter diagnostics: %v", err)
	}
}
