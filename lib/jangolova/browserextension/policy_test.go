package browserextension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxPolicyForceAndBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed", "jangolova-extensions.json")
	for _, action := range []string{"force", "block"} {
		if _, err := manageLinuxPolicy(exampleExtensionID, chromeUpdateURL, action, path); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var policies map[string][]string
	if err := json.Unmarshal(content, &policies); err != nil {
		t.Fatal(err)
	}
	if got := policies["ExtensionInstallForcelist"]; len(got) != 1 || got[0] != exampleExtensionID+";"+chromeUpdateURL {
		t.Fatalf("wrong force policy: %+v", policies)
	}
	if got := policies["ExtensionInstallBlocklist"]; len(got) != 1 || got[0] != exampleExtensionID {
		t.Fatalf("wrong block policy: %+v", policies)
	}
	for _, action := range []string{"unforce", "unblock"} {
		if _, err := manageLinuxPolicy(exampleExtensionID, chromeUpdateURL, action, path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty policy file remains: %v", err)
	}
}

func TestLinuxPolicyRefusesCompetingFile(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "other.json")
	if err := os.WriteFile(other, []byte(`{"ExtensionInstallForcelist":["other"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := manageLinuxPolicy(exampleExtensionID, chromeUpdateURL, "force", filepath.Join(dir, "jangolova-extensions.json")); err == nil {
		t.Fatal("added competing policy")
	}
}

func TestWindowsPolicyScriptKeepsExistingValues(t *testing.T) {
	path, script := windowsPolicyScript("chrome", exampleExtensionID, chromeUpdateURL, "force")
	if !strings.Contains(path, `Google\Chrome\ExtensionInstallForcelist`) || !strings.Contains(script, "while ($entries.Name -contains") || !strings.Contains(script, "New-ItemProperty") {
		t.Fatalf("bad policy script: %s %s", path, script)
	}
	_, script = windowsPolicyScript("edge", exampleExtensionID, edgeUpdateURL, "unblock")
	if !strings.Contains(script, "Remove-ItemProperty") || strings.Contains(script, "Remove-Item -LiteralPath $path") {
		t.Fatalf("policy removal would delete more than its entry: %s", script)
	}
}
