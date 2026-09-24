package browserextension

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallWithNativeUIVerifiesRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pipe-based browser adapter is not implemented on Windows")
	}
	source := fixture(t)
	prepared, err := Inspect(source)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	staged := filepath.Join(root, "staged")
	launchCount := filepath.Join(root, "launch-count")
	if err := os.WriteFile(launchCount, []byte("0"), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(root, "browser")
	quoted := "'" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "'"
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec "+quoted+" -test.run=^TestNativeBrowserHelper$\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CYMONKEY_FAKE_BROWSER", "1")
	t.Setenv("CYMONKEY_FAKE_BROWSER_COUNT", launchCount)
	t.Setenv("CYMONKEY_FAKE_BROWSER_EXTENSION", staged)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var statuses []string
	err = InstallWithNativeUI(ctx, DevToolsTarget{
		Browser: "chrome", ExecutablePath: launcher,
		ProfilePath: filepath.Join(root, "user-data"), ProfileDirectory: "Profile 2",
	}, source, staged, prepared.Revision, func(result InstallResult) error {
		statuses = append(statuses, result.Status)
		if result.Status == "installed" {
			if result.ID != "abcdefghijklmnopabcdefghijklmnop" || result.Profile != filepath.Join(root, "user-data", "Profile 2") {
				t.Errorf("incorrect installed result: %+v", result)
			}
			cancel()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0] != "awaiting-browser-action" || statuses[1] != "installed" {
		t.Fatalf("incorrect install statuses: %v", statuses)
	}
	count, err := os.ReadFile(launchCount)
	if err != nil || string(count) != "2" {
		t.Fatalf("browser was not restarted: count=%s error=%v", count, err)
	}
}

// This process stands in for the browser's pipe protocol in the restart test.
func TestNativeBrowserHelper(t *testing.T) {
	if os.Getenv("CYMONKEY_FAKE_BROWSER") != "1" {
		t.Skip("helper process")
	}
	countPath := os.Getenv("CYMONKEY_FAKE_BROWSER_COUNT")
	count, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatal(err)
	}
	launch := 0
	if _, err := fmt.Sscanf(string(count), "%d", &launch); err != nil {
		t.Fatal(err)
	}
	launch++
	if err := os.WriteFile(countPath, []byte(fmt.Sprint(launch)), 0600); err != nil {
		t.Fatal(err)
	}
	request := os.NewFile(3, "request")
	response := os.NewFile(4, "response")
	reader := bufio.NewReader(request)
	queries := 0
	for {
		message, err := reader.ReadBytes(0)
		if err != nil {
			return
		}
		var call struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(bytes.TrimSuffix(message, []byte{0}), &call); err != nil {
			t.Fatal(err)
		}
		if call.Method == "Browser.close" {
			return
		}
		queries++
		items := []nativeExtension{}
		if launch > 1 || queries > 1 {
			items = append(items, nativeExtension{ID: "abcdefghijklmnopabcdefghijklmnop", Path: os.Getenv("CYMONKEY_FAKE_BROWSER_EXTENSION"), Enabled: true})
		}
		encoded, err := json.Marshal(map[string]any{"id": call.ID, "result": map[string]any{"extensions": items}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := response.Write(append(encoded, 0)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSameExtensionPathRejectsUnrelatedDirectory(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "staged")
	other := filepath.Join(root, "other")
	if err := os.Mkdir(staged, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	if !sameExtensionPath(staged, staged) || sameExtensionPath(other, staged) || sameExtensionPath("relative", staged) {
		t.Fatal("incorrect extension path match")
	}
}

func TestDefaultChromeUserDataDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if isDefaultChromeUserDataDir(filepath.Join(t.TempDir(), "custom-data")) {
		t.Fatal("custom Chrome data directory was rejected")
	}
	if runtime.GOOS == "darwin" && !isDefaultChromeUserDataDir(filepath.Join(home, "Library", "Application Support", "Google", "Chrome")) {
		t.Fatal("default macOS Chrome data directory was not detected")
	}
}
