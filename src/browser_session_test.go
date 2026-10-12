package main

import (
	"context"
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
