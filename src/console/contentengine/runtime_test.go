package contentengine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRuntimeInstallResolveAndTamperDetection(t *testing.T) {
	source := t.TempDir()
	root := t.TempDir()
	payload := []byte("#!/bin/sh\nexit 0\n")
	path := filepath.Join(source, "bin", "python3")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	manifest := RuntimeManifest{
		APIVersion: RuntimeManifestAPIVersion,
		Name:       "python",
		Version:    "3.12.8",
		Platform:   runtime.GOOS + "-" + runtime.GOARCH,
		Executable: "bin/python3",
		Files:      []RuntimeFile{{Path: "bin/python3", SHA256: hex.EncodeToString(digest[:]), Executable: true}},
	}
	encoded, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(source, "runtime.json")
	if err := os.WriteFile(manifestPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	installed, err := InstallRuntime(root, manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if installed.ExecutablePath() != filepath.Join(installed.Directory, "bin", "python3") {
		t.Fatalf("executable = %q", installed.ExecutablePath())
	}
	if _, err := ResolveInstalledRuntime(root, "python", "3.12.8"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed.ExecutablePath(), []byte("changed"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveInstalledRuntime(root, "python", "3.12.8"); err == nil {
		t.Fatal("tampered runtime remained loadable")
	}
}

func TestBuildRuntimeManifestInventoriesTree(t *testing.T) {
	source := t.TempDir()
	path := filepath.Join(source, "bin", "python3")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "stdlib.py"), []byte("library"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildRuntimeManifest(source, "python", "3.12.8", "bin/python3")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 2 || manifest.Executable != "bin/python3" {
		t.Fatalf("runtime manifest = %#v", manifest)
	}
}
