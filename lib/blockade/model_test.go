package blockade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeModelFixture(t *testing.T, provider string) string {
	t.Helper()
	dir := t.TempDir()
	artifact := []byte("model weights")
	digest := sha256.Sum256(artifact)
	if err := os.WriteFile(filepath.Join(dir, "weights.gguf"), artifact, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := ModelManifest{
		APIVersion: ModelManifestAPIVersion,
		Name:       "fixture-model",
		Version:    "1.0.0",
		Provider:   provider,
		Capabilities: []string{
			CapabilityTextInterpret,
		},
		Artifacts: []ModelArtifact{{Role: "weights", File: "weights.gguf", SHA256: hex.EncodeToString(digest[:])}},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "model.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstallResolveAndListModel(t *testing.T) {
	manifestPath := writeModelFixture(t, "fixture")
	root := t.TempDir()
	inspected := false
	installed, err := InstallModel(context.Background(), root, manifestPath, func(_ context.Context, model InstalledModel) error {
		inspected = true
		if model.Manifest.Provider != "fixture" || !filepath.IsAbs(model.Directory) {
			t.Fatalf("staged model = %#v", model)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inspected || installed.Manifest.Name != "fixture-model" {
		t.Fatalf("installed model = %#v, inspected = %v", installed, inspected)
	}
	resolved, err := ResolveInstalledModel(root, "fixture", "fixture-model@1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Directory != installed.Directory {
		t.Fatalf("resolved directory = %q, want %q", resolved.Directory, installed.Directory)
	}
	models, err := ListInstalledModels(root)
	if err != nil || len(models) != 1 || models[0].Manifest.Name != "fixture-model" {
		t.Fatalf("models = %#v, %v", models, err)
	}
	if err := os.WriteFile(filepath.Join(installed.Directory, "weights.gguf"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveInstalledModel(root, "fixture", "fixture-model@1.0.0"); err == nil {
		t.Fatal("expected changed model artifact to fail verification")
	}
}

func TestInstallModelRequiresProviderAcceptance(t *testing.T) {
	manifestPath := writeModelFixture(t, "fixture")
	root := t.TempDir()
	if _, err := InstallModel(context.Background(), root, manifestPath, func(context.Context, InstalledModel) error {
		return ErrModalityUnsupported
	}); err == nil {
		t.Fatal("expected provider rejection")
	}
	models, err := ListInstalledModels(root)
	if err != nil || len(models) != 0 {
		t.Fatalf("rejected models = %#v, %v", models, err)
	}
}

func TestRemoveInstalledModel(t *testing.T) {
	manifestPath := writeModelFixture(t, "fixture")
	root := filepath.Join(t.TempDir(), "models")
	installed, err := InstallModel(context.Background(), root, manifestPath, func(context.Context, InstalledModel) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveInstalledModel(root, installed.Manifest.Provider, installed.Manifest.Name+"@"+installed.Manifest.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installed.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed model still exists: %v", err)
	}
}
