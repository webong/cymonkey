package browserextension

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirefoxBiDiConnection(t *testing.T) {
	executable := os.Getenv("CYMONKEY_TEST_FIREFOX_BIN")
	if executable == "" {
		t.Skip("set CYMONKEY_TEST_FIREFOX_BIN to test the installed Firefox")
	}
	session, err := startFirefox(context.Background(), DevToolsTarget{Browser: "firefox", ExecutablePath: executable, ProfilePath: filepath.Join(t.TempDir(), "profile")})
	if err != nil {
		t.Fatal(err)
	}
	defer session.stop()
	var status struct {
		Ready bool `json:"ready"`
	}
	if err := session.call(context.Background(), "session.status", map[string]any{}, &status); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "unsigned.xpi")
	if _, err := Package(fixture(t), archive); err != nil {
		t.Fatal(err)
	}
	var installed struct {
		Extension string `json:"extension"`
	}
	err = session.call(context.Background(), "webExtension.install", map[string]any{
		"extensionData": map[string]string{"type": "archivePath", "path": archive}, "moz:permanent": true,
	}, &installed)
	if err == nil || strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("Firefox did not reject an unsigned permanent XPI through BiDi: %v", err)
	}
	if err := session.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFirefoxProfileVerification(t *testing.T) {
	source := fixture(t)
	profile := t.TempDir()
	id := "example@cymonkey.test"
	archive := filepath.Join(profile, "extensions", id+".xpi")
	if _, err := Package(source, archive); err != nil {
		t.Fatal(err)
	}
	description, err := Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string]any{"addons": []any{map[string]any{
		"id": id, "version": description.Version, "type": "extension", "path": archive,
		"active": true, "userDisabled": false, "appDisabled": false,
		"defaultLocale": map[string]string{"name": description.Name},
	}}}
	data, _ := json.Marshal(metadata)
	if err := os.WriteFile(filepath.Join(profile, "extensions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFirefoxProfile(profile, id, description); err != nil {
		t.Fatal(err)
	}
	metadata["addons"] = []any{map[string]any{"id": id, "version": description.Version, "type": "extension", "path": archive, "active": false}}
	data, _ = json.Marshal(metadata)
	if err := os.WriteFile(filepath.Join(profile, "extensions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFirefoxProfile(profile, id, description); err == nil {
		t.Fatal("accepted a disabled extension")
	}
}
