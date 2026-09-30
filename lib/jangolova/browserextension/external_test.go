package browserextension

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const exampleExtensionID = "abcdefghijklmnopabcdefghijklmnop"

func TestUnixStoreRequestAndRemoval(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	directory := filepath.Join(root, ".config", "microsoft-edge", "External Extensions")
	result, err := changeStoreInstall("linux", "edge", "chrome", exampleExtensionID, directory, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "requested" || result.Browser != "edge" {
		t.Fatalf("unexpected result: %+v", result)
	}
	content, err := os.ReadFile(result.Source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), chromeUpdateURL) {
		t.Fatalf("wrong store: %s", content)
	}
	if _, err := changeStoreInstall("linux", "edge", "edge", exampleExtensionID, directory, true); err == nil {
		t.Fatal("removed a request for a different store")
	}
	removed, err := changeStoreInstall("linux", "edge", "chrome", exampleExtensionID, directory, true)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Status != "request-removed" {
		t.Fatalf("unexpected removal: %+v", removed)
	}
	if _, err := os.Stat(result.Source); !os.IsNotExist(err) {
		t.Fatalf("request still present: %v", err)
	}
}

func TestExternalRequestRejectsWrongBrowserAndPath(t *testing.T) {
	if _, err := changeStoreInstall("linux", "firefox", "", exampleExtensionID, "", false); err == nil {
		t.Fatal("accepted Firefox")
	}
	if _, err := changeStoreInstall("linux", "chrome", "edge", exampleExtensionID, "", false); err == nil {
		t.Fatal("accepted Edge Add-ons for Chrome")
	}
	if _, err := changeStoreInstall("linux", "edge", "edge", exampleExtensionID, filepath.Join(t.TempDir(), "arbitrary"), false); err == nil {
		t.Fatal("accepted arbitrary directory")
	}
	if _, err := changeStoreInstall("windows", "edge", "", exampleExtensionID, "C:\\Extensions", false); err == nil {
		t.Fatal("accepted a directory on Windows")
	}
}

func TestWindowsPowerShellRegistryScript(t *testing.T) {
	path, script := windowsExternalScript("edge", exampleExtensionID, edgeUpdateURL, false)
	if !strings.Contains(path, `Microsoft\Edge\Extensions\`+exampleExtensionID) || !strings.Contains(script, "New-ItemProperty") || !strings.Contains(script, edgeUpdateURL) {
		t.Fatalf("wrong Windows request: %s %s", path, script)
	}
	encoded, err := base64.StdEncoding.DecodeString(encodePowerShell(script))
	if err != nil || len(encoded)%2 != 0 || encoded[0] != '$' || encoded[1] != 0 {
		t.Fatalf("PowerShell command is not UTF-16LE: %v", err)
	}
	_, script = windowsExternalScript("chrome", exampleExtensionID, chromeUpdateURL, true)
	if !strings.Contains(script, "Remove-Item") || !strings.Contains(script, "different update URL") {
		t.Fatalf("removal is not guarded: %s", script)
	}
}
