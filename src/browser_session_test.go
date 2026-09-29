package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jangolova/browserextension"
)

func TestBrowserSessionUsesActiveProfileEndpoint(t *testing.T) {
	profile := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile, "DevToolsActivePort"), []byte("9222\n/devtools/browser/fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	request := browserSessionRequest{Browser: "chromium", Profile: profile, Adapter: "fixture"}
	connection, target, err := request.connectRequest()
	if err != nil {
		t.Fatal(err)
	}
	if target != "" || connection.Target.Endpoints[0].Protocol != "cdp" || connection.Target.Endpoints[0].URL != "http://127.0.0.1:9222" || !strings.HasPrefix(connection.InstanceID, "browser-") {
		t.Fatalf("connection = %#v, userscripts target = %q", connection, target)
	}
}

func TestBrowserSessionNeedsAnExistingEndpoint(t *testing.T) {
	_, _, err := (browserSessionRequest{Browser: "firefox", Profile: t.TempDir()}).connectRequest()
	if err == nil || !strings.Contains(err.Error(), "requires an endpoint") {
		t.Fatalf("missing endpoint error = %v", err)
	}
	_, _, err = (browserSessionRequest{Browser: "chromium", Profile: t.TempDir()}).connectRequest()
	if err == nil || !strings.Contains(err.Error(), "no active CDP endpoint") {
		t.Fatalf("missing CDP endpoint error = %v", err)
	}
}

func TestBrowserSessionRejectsTargetAndPathMix(t *testing.T) {
	_, err := (browserSessionRequest{TargetID: "chrome:fixture", Browser: "chrome"}).selectedTarget()
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("mixed selection error = %v", err)
	}
	_, _, err = splitBrowserEndpoint("https://127.0.0.1:9222")
	if err == nil {
		t.Fatal("accepted endpoint without protocol")
	}
	_, _, err = browserextension.ActiveEndpoint(browserextension.BrowserTarget{Browser: "firefox", ProfilePath: t.TempDir()})
	if err == nil {
		t.Fatal("guessed Firefox endpoint from profile")
	}
}
