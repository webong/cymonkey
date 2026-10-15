package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserSessionUsesTheEndpointTheAdapterReports(t *testing.T) {
	installFakeAdapter(t, "chrome", chromeOperations, map[string]string{
		"session.targets": managementResponse("session", "targets", "ready", `[{"id":"page-1","protocol":"cdp","endpoint":"ws://127.0.0.1:9222/devtools/browser/fixture","url":"https://example.com"}]`),
	}, nil)
	request := browserSessionRequest{Browser: "chrome", Profile: t.TempDir(), Adapter: "fixture"}
	connection, target, err := request.connectRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := connection.Target.Endpoints[0]
	if target != "" || endpoint.Protocol != "cdp" || endpoint.URL != "ws://127.0.0.1:9222/devtools/browser/fixture" || !strings.HasPrefix(connection.InstanceID, "browser-") {
		t.Fatalf("connection = %#v, userscripts target = %q", connection, target)
	}
}

func TestBrowserSessionNeedsADebuggablePage(t *testing.T) {
	installFakeAdapter(t, "chrome", chromeOperations, map[string]string{
		"session.targets": managementResponse("session", "targets", "ready", `[]`),
	}, nil)
	_, _, err := (browserSessionRequest{Browser: "chrome", Profile: t.TempDir()}).connectRequest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no debuggable page") {
		t.Fatalf("no page error = %v", err)
	}
}

func TestBrowserSessionRejectsTargetAndPathMix(t *testing.T) {
	_, err := (browserSessionRequest{TargetID: "chrome:fixture", Browser: "chrome"}).selectedTarget(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("mixed selection error = %v", err)
	}
	if _, _, err = splitBrowserEndpoint("https://127.0.0.1:9222"); err == nil {
		t.Fatal("accepted endpoint without protocol")
	}
}

func TestBrowserSessionPrefersTheAdaptersEndpointOperation(t *testing.T) {
	// No page is open: session.targets would report nothing, but the adapter
	// declares session.endpoint and answers without one.
	operations := append([]string{"session.endpoint"}, chromeOperations...)
	adapter := installFakeAdapter(t, "chrome", operations, map[string]string{
		"session.endpoint": managementResponse("session", "endpoint", "ready", `{"protocol":"cdp","endpoint":"ws://127.0.0.1:9333/devtools/browser/secret-id"}`),
		"session.targets":  managementResponse("session", "targets", "ready", `[]`),
	}, nil)
	request := browserSessionRequest{Browser: "chrome", Profile: t.TempDir()}
	connection, _, err := request.connectRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if endpoint := connection.Target.Endpoints[0]; endpoint.Protocol != "cdp" || endpoint.URL != "ws://127.0.0.1:9333/devtools/browser/secret-id" {
		t.Fatalf("connection endpoint = %q %q", endpoint.Protocol, endpoint.URL)
	}
	if _, err := os.Stat(filepath.Join(adapter.home, "session.targets.seen")); err == nil {
		t.Fatal("session.targets was used although the adapter declares session.endpoint")
	}
}

func TestBrowserSessionRefusesAnEndpointOffTheLoopbackAndNeverEchoesIt(t *testing.T) {
	operations := append([]string{"session.endpoint"}, chromeOperations...)
	installFakeAdapter(t, "chrome", operations, map[string]string{
		"session.endpoint": managementResponse("session", "endpoint", "ready", `{"protocol":"cdp","endpoint":"ws://203.0.113.9:9333/devtools/browser/secret-id"}`),
	}, nil)
	_, _, err := (browserSessionRequest{Browser: "chrome", Profile: t.TempDir()}).connectRequest(context.Background())
	if err == nil {
		t.Fatal("a non-loopback endpoint was accepted")
	}
	if strings.Contains(err.Error(), "secret-id") || strings.Contains(err.Error(), "203.0.113.9") {
		t.Fatalf("error leaked the endpoint: %v", err)
	}
}
