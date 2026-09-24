package browserextension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStageForChromeReportsBrowserHandoff(t *testing.T) {
	result, err := StageForChrome(fixture(t), filepath.Join(t.TempDir(), "staged"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "awaiting-browser-action" || result.Extension.Name != "Example" {
		t.Fatalf("unexpected handoff: %+v", result)
	}
}

func TestInstallCapabilityFollowsSelectedBrowser(t *testing.T) {
	for _, browserName := range []string{"chrome", "chromium", "edge"} {
		capability := Capability(browserName)
		if (runtime.GOOS != "windows" && (!capability.PersistentLocalInstall || capability.InstallDriver != "native-load-unpacked" || !capability.RequiresBrowserAction || !capability.SessionLoad || capability.SessionDriver != "cdp-pipe")) || (runtime.GOOS == "windows" && (capability.PersistentLocalInstall || capability.SessionLoad)) {
			t.Fatalf("incorrect session adapter for %s: %+v", browserName, capability)
		}
	}
	if !Capability("firefox").PersistentLocalInstall || Capability("firefox").SessionLoad || Capability("firefox").InstallDriver != "webdriver-bidi-signed-xpi" {
		t.Fatal("incorrect Firefox installation capability")
	}
	if Capability("safari").SessionLoad || (runtime.GOOS == "darwin" && !Capability("safari").PersistentLocalInstall) {
		t.Fatal("incorrect Safari installation capability")
	}
}

func TestStageForEdgeReportsCorrectBrowser(t *testing.T) {
	result, err := StageForChromium("edge", fixture(t), filepath.Join(t.TempDir(), "staged"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Browser != "edge" || !strings.Contains(result.NextAction, "edge://extensions") {
		t.Fatalf("unexpected Edge handoff: %+v", result)
	}
	if _, err := StageForChromium("safari", fixture(t), filepath.Join(t.TempDir(), "staged")); err == nil {
		t.Fatal("accepted unsupported browser")
	}
}

func TestRequestChromeWebStoreInstall(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS external preference route")
	}
	id := "abcdefghijklmnopabcdefghijklmnop"
	directory := filepath.Join(t.TempDir(), "Library", "Application Support", "Google", "Chrome", "External Extensions")
	result, err := RequestChromeWebStoreInstall(id, directory)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "awaiting-browser-confirmation" {
		t.Fatalf("unexpected result: %+v", result)
	}
	content, err := os.ReadFile(filepath.Join(directory, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]string
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatal(err)
	}
	if config["external_update_url"] != "https://clients2.google.com/service/update2/crx" {
		t.Fatalf("bad config: %+v", config)
	}
	if _, err := RequestChromeWebStoreInstall(id, directory); err == nil {
		t.Fatal("overwrote an existing installation request")
	}
	if _, err := RequestChromeWebStoreInstall("invalid", directory); err == nil {
		t.Fatal("accepted invalid ID")
	}
}

func TestRequestChromeWebStoreRejectsSymlinkedPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS external preference route")
	}
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	if err := os.Mkdir(actual, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	if _, err := RequestChromeWebStoreInstall("abcdefghijklmnopabcdefghijklmnop", filepath.Join(link, "Library", "Application Support", "Google", "Chrome", "External Extensions")); err == nil {
		t.Fatal("accepted symlinked path")
	}
}
