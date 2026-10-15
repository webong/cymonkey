package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCookiesQueryGoesThroughTheAdapterAndNormalizeNeedsAStore(t *testing.T) {
	chrome := installFakeAdapter(t, "chrome", chromeOperations, map[string]string{
		"cookie.query": `{"cookies":[{"name":"session","value":"synthetic-secret","domain":"example.com","path":"/","expiry":0,"secure":true,"http_only":true}],"store_id":"0"}`,
	}, nil)
	var out bytes.Buffer
	if err := cookiesCommand([]string{"query", "--browser", "chrome", "--profile-directory", "Default", "--site", "https://example.com/", "--name", "session"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"value":"synthetic-secret"`) {
		t.Fatalf("output = %s", out.String())
	}
	seen := chrome.seen("cookie.query")
	for _, want := range []string{`"site":"https://example.com/"`, `"names":["session"]`, `"version":2`} {
		if !strings.Contains(seen, want) {
			t.Fatalf("adapter request %s lacks %s", seen, want)
		}
	}
	if chrome.profile("cookie.query") != "Default" {
		t.Fatalf("profile = %q", chrome.profile("cookie.query"))
	}
	if err := cookiesCommand([]string{"query", "--browser", "chrome"}, &out); err == nil || !strings.Contains(err.Error(), "--site") {
		t.Fatalf("query without a site error = %v", err)
	}
	if err := cookiesCommand([]string{"normalize", "--browser", "chrome", "--store-id", "0"}, &out); err == nil {
		t.Fatal("normalize accepted no input")
	}
	export := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(export, []byte(`{"selection":{"browser":"chrome"},"cookies":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cookiesCommand([]string{"normalize", "--browser", "chrome", "--store-id", "0", "--input-file", export}, &out); err == nil {
		t.Fatal("a missing canned response reported success")
	}
	if !strings.Contains(chrome.seen("cookie.normalize"), `"store_id":"0"`) {
		t.Fatalf("normalize request = %s", chrome.seen("cookie.normalize"))
	}
}
