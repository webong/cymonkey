package main

import (
	"bytes"
	"strings"
	"testing"
)

var chromeOperations = []string{
	"extension.targets", "extension.capabilities", "extension.prepare", "extension.package", "extension.sign",
	"extension.stage", "extension.install", "extension.activate", "extension.store_install", "extension.store_remove",
	"extension.policy", "session.targets",
}

func TestExtensionStoreRequestsGoToTheNamedAdapter(t *testing.T) {
	chrome := installFakeAdapter(t, "chrome", chromeOperations, map[string]string{
		"extension.store_install": managementResponse("extension", "store_install", "requested", `{"status":"requested","browser":"chrome"}`),
		"extension.store_remove":  managementResponse("extension", "store_remove", "requested", `{"status":"requested"}`),
	}, nil)
	var out bytes.Buffer
	if err := extensionCommand([]string{"install-store", "--browser", "chrome", "--id", "abcdefghijklmnopabcdefghijklmnop", "--store", "chrome"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"requested"`) {
		t.Fatalf("install-store output = %s", out.String())
	}
	seen := chrome.seen("extension.store_install")
	for _, want := range []string{`"id":"abcdefghijklmnopabcdefghijklmnop"`, `"store":"chrome"`, `"kind":"extension"`, `"action":"store_install"`} {
		if !strings.Contains(seen, want) {
			t.Fatalf("adapter request %s lacks %s", seen, want)
		}
	}
	// Store requests choose a browser installation, never a profile.
	err := extensionCommand([]string{"remove-store", "--browser", "chrome", "--profile", t.TempDir(), "--id", "x"}, &out)
	if err == nil || !strings.Contains(err.Error(), "installation") {
		t.Fatalf("store request with a profile error = %v", err)
	}
	if err := extensionCommand([]string{"remove-store", "--browser", "chrome", "--id", "abcdefghijklmnopabcdefghijklmnop"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chrome.seen("extension.store_remove"), `"action":"store_remove"`) {
		t.Fatal("remove-store did not reach the adapter")
	}
}

func TestExtensionPolicyMapsModeToThePolicyAction(t *testing.T) {
	chrome := installFakeAdapter(t, "chrome", chromeOperations, map[string]string{
		"extension.policy": managementResponse("extension", "policy", "applied", `{"status":"applied"}`),
	}, nil)
	var out bytes.Buffer
	if err := extensionCommand([]string{"policy", "--browser", "chrome", "--id", "abc", "--mode", "force"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chrome.seen("extension.policy"), `"policyAction":"force"`) {
		t.Fatalf("policy request = %s", chrome.seen("extension.policy"))
	}
	if err := extensionCommand([]string{"policy", "--browser", "chrome", "--id", "abc", "--mode", "force", "--external-dir", "/x"}, &out); err == nil {
		t.Fatal("policy accepted an external extensions directory")
	}
}

func TestExtensionInstallStreamsProgressThenTheResult(t *testing.T) {
	chrome := installFakeAdapter(t, "chrome", chromeOperations, map[string]string{
		"extension.install": managementResponse("extension", "install", "awaiting-browser-action", `{"status":"awaiting-browser-action","nextAction":"Load unpacked"}`),
	}, map[string]string{
		"extension.install": "browser management progress: {\"status\":\"staged\"}\n",
	})
	var out bytes.Buffer
	err := extensionCommand([]string{"install", "--browser", "chrome", "--profile-directory", "Default", "--source", "/ext", "--revision", "sha256:abc"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || lines[0] != `{"status":"staged"}` || !strings.Contains(lines[1], "awaiting-browser-action") {
		t.Fatalf("install output = %q", out.String())
	}
	if chrome.profile("extension.install") != "Default" {
		t.Fatalf("adapter profile = %q", chrome.profile("extension.install"))
	}
	if strings.Contains(out.String(), `"installed"`) {
		t.Fatal("a handoff was reported as installed")
	}
}

func TestExtensionSigningPassesTheExecutableAndKey(t *testing.T) {
	chrome := installFakeAdapter(t, "chrome", chromeOperations, map[string]string{
		"extension.sign": managementResponse("extension", "sign", "signed", `{"id":"abc","keyPath":"/k.pem"}`),
	}, nil)
	var out bytes.Buffer
	err := extensionCommand([]string{"sign", "--browser", "chrome", "--browser-bin", "/Applications/Chrome", "--source", "/ext", "--output", "/out.crx", "--revision", "sha256:abc", "--key", "/k.pem"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	seen := chrome.seen("extension.sign")
	for _, want := range []string{`"executable":"/Applications/Chrome"`, `"keyPath":"/k.pem"`, `"output":"/out.crx"`, `"revision":"sha256:abc"`} {
		if !strings.Contains(seen, want) {
			t.Fatalf("sign request %s lacks %s", seen, want)
		}
	}
}

func TestSafariPackagingUsesTheSafariAdapterWithoutANamedBrowser(t *testing.T) {
	chrome := installFakeAdapter(t, "chrome", chromeOperations, nil, nil)
	safari := installFakeAdapter(t, "safari", []string{"extension.convert", "extension.sign"}, map[string]string{
		"extension.convert": managementResponse("extension", "convert", "prepared", `{"project":"/p"}`),
		"extension.sign":    managementResponse("extension", "sign", "signed", `{"app":"/a.app"}`),
	}, nil)
	var out bytes.Buffer
	if err := extensionCommand([]string{"package-safari", "--source", "/ext", "--output", "/p", "--revision", "sha256:abc", "--bundle-id", "com.example.x", "--app-name", "X"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(safari.seen("extension.convert"), `"bundleId":"com.example.x"`) {
		t.Fatalf("convert request = %s", safari.seen("extension.convert"))
	}
	if err := extensionCommand([]string{"build-safari", "--project", "/p/X.xcodeproj", "--scheme", "X", "--output", "/o", "--team-id", "TEAM"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(safari.seen("extension.sign"), `"teamId":"TEAM"`) {
		t.Fatalf("build request = %s", safari.seen("extension.sign"))
	}
	_ = chrome
}

func TestPrepareNeedsABrowserWhenSeveralAdaptersDeclareIt(t *testing.T) {
	installFakeAdapter(t, "chrome", chromeOperations, map[string]string{"extension.prepare": managementResponse("extension", "prepare", "prepared", `{"revision":"r"}`)}, nil)
	installFakeAdapter(t, "edge", chromeOperations, map[string]string{"extension.prepare": managementResponse("extension", "prepare", "prepared", `{"revision":"r"}`)}, nil)
	var out bytes.Buffer
	err := extensionCommand([]string{"prepare", "--source", "/ext"}, &out)
	if err == nil || !strings.Contains(err.Error(), "--browser") {
		t.Fatalf("ambiguous prepare error = %v", err)
	}
	if err := extensionCommand([]string{"prepare", "--browser", "edge", "--source", "/ext"}, &out); err != nil {
		t.Fatal(err)
	}
}

func TestTargetsAreDiscoveredAcrossTrustedAdaptersAndSelectedByID(t *testing.T) {
	chrome := installFakeAdapter(t, "chrome", chromeOperations, map[string]string{
		"extension.targets":      managementResponse("extension", "targets", "ready", `[{"id":"chrome:aaa","browser":"chrome","name":"Chrome","profile":"Default","executablePath":"/c","profilePath":"/p","installMode":"native"}]`),
		"extension.capabilities": managementResponse("extension", "capabilities", "ready", `{"browser":"chrome"}`),
	}, nil)
	installFakeAdapter(t, "firefox", []string{"extension.targets"}, map[string]string{
		"extension.targets": managementResponse("extension", "targets", "ready", `[{"id":"firefox:bbb","browser":"firefox","name":"Firefox","profile":"dev","executablePath":"/f","installMode":"native"}]`),
	}, nil)
	var out bytes.Buffer
	if err := browserCommand([]string{"targets"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "chrome:aaa") || !strings.Contains(out.String(), "firefox:bbb") || !strings.Contains(out.String(), `"adapter":"firefox"`) {
		t.Fatalf("targets = %s", out.String())
	}
	out.Reset()
	if err := extensionCommand([]string{"capabilities", "--target", "chrome:aaa"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chrome.seen("extension.capabilities"), `"targetId":"chrome:aaa"`) || chrome.profile("extension.capabilities") != "Default" {
		t.Fatalf("capabilities request = %s profile %q", chrome.seen("extension.capabilities"), chrome.profile("extension.capabilities"))
	}
	if err := extensionCommand([]string{"capabilities", "--target", "chrome:aaa", "--browser", "chrome"}, &out); err == nil {
		t.Fatal("a target ID was combined with a browser")
	}
	if err := extensionCommand([]string{"capabilities", "--target", "missing:zzz"}, &out); err == nil {
		t.Fatal("an unknown target resolved")
	}
}

func TestUntrustedAdaptersNeverRunAndAreSkippedInDiscovery(t *testing.T) {
	adapter := installFakeAdapter(t, "chrome", chromeOperations, map[string]string{
		"extension.targets": managementResponse("extension", "targets", "ready", `[]`),
	}, nil)
	if err := browserHost().Store().RemoveTrust("chrome"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := browserCommand([]string{"targets"}, &out); err != nil || strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("targets with only an untrusted adapter = %q %v", out.String(), err)
	}
	if err := extensionCommand([]string{"prepare", "--browser", "chrome", "--source", "/ext"}, &out); err == nil {
		t.Fatal("an untrusted adapter ran")
	}
	_ = adapter
}
