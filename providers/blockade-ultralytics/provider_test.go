package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blockade"
)

func TestInspectionRejectsIncompleteBundle(t *testing.T) {
	model := blockade.InstalledModel{Manifest: blockade.ModelManifest{
		APIVersion: blockade.ModelManifestAPIVersion, Name: "vision", Version: "1.0.0", Provider: "local-ultralytics",
		Capabilities: []string{blockade.CapabilityObjectDetect, blockade.CapabilityObjectSegment},
		Artifacts:    []blockade.ModelArtifact{{Role: "yolo", File: "yolo.pt", SHA256: strings.Repeat("0", 64)}},
	}, Directory: t.TempDir()}
	if err := inspectInstalledModel(model, "local-ultralytics"); err == nil {
		t.Fatal("expected missing SAM artifact to fail")
	}
}

func TestWorkerSettings(t *testing.T) {
	model := blockade.InstalledModel{Manifest: blockade.ModelManifest{
		APIVersion: blockade.ModelManifestAPIVersion, Name: "vision", Version: "1.0.0", Provider: "local-ultralytics",
		Capabilities: []string{blockade.CapabilityObjectDetect, blockade.CapabilityObjectSegment},
		Artifacts: []blockade.ModelArtifact{
			{Role: "yolo", File: "yolo.pt", SHA256: strings.Repeat("0", 64)},
			{Role: "sam", File: "sam.pt", SHA256: strings.Repeat("1", 64)},
		},
	}, Directory: t.TempDir()}
	for _, name := range []string{"yolo.pt", "sam.pt"} {
		if err := os.WriteFile(filepath.Join(model.Directory, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	config, err := workerConfigFrom(model, map[string]string{"command": `["python3","worker.py"]`, "workers": "2"})
	if err != nil {
		t.Fatal(err)
	}
	if config.Workers != 2 || len(config.Command) != 2 || len(config.Env) != 2 {
		t.Fatalf("worker config = %#v", config)
	}
}

func TestEmbeddedWorkerIsPackagedWithProvider(t *testing.T) {
	command, cleanup, err := embeddedWorkerCommand()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(command) != 2 || command[0] != "python3" {
		t.Fatalf("embedded worker command = %#v", command)
	}
	for _, name := range []string{"worker.py", "runtime.py"} {
		if info, err := os.Stat(filepath.Join(filepath.Dir(command[1]), name)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("embedded %s: %v", name, err)
		}
	}
}
