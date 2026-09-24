package browserextension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverTargetsFromLocalBrowserProfiles(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	apps := filepath.Join(root, "Applications")
	for _, suffix := range []string{
		"Google Chrome.app/Contents/MacOS/Google Chrome",
		"Firefox.app/Contents/MacOS/firefox",
		"Safari.app/Contents/MacOS/Safari",
	} {
		path := filepath.Join(apps, suffix)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("browser"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	chromeRoot := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	for _, name := range []string{"Default", "Profile 2"} {
		if err := os.MkdirAll(filepath.Join(chromeRoot, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	firefoxRoot := filepath.Join(home, "Library", "Application Support", "Firefox")
	if err := os.MkdirAll(filepath.Join(firefoxRoot, "Profiles", "abcd.default"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(firefoxRoot, "profiles.ini"), []byte("[Profile0]\nName=Personal\nIsRelative=1\nPath=Profiles/abcd.default\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first := discoverTargets("darwin", home, []string{apps})
	second := discoverTargets("darwin", home, []string{apps})
	expectedFirefoxProfile, err := filepath.EvalSymlinks(filepath.Join(firefoxRoot, "Profiles", "abcd.default"))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 4 || len(second) != len(first) {
		t.Fatalf("unexpected targets: %+v", first)
	}
	for i, target := range first {
		if target.ID == "" || target.ID != second[i].ID {
			t.Fatalf("unstable target ID: %+v", target)
		}
		if target.Browser == "firefox" && (target.ProfilePath != expectedFirefoxProfile || !strings.Contains(target.Name, "Personal")) {
			t.Fatalf("wrong Firefox profile: %+v", target)
		}
		if target.Browser == "safari" && (target.ProfilePath != "" || target.InstallMode != "app-handoff") {
			t.Fatalf("Safari incorrectly has a selectable profile: %+v", target)
		}
	}
}
