package providerplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallReverifiesExecutableAndRejectsTampering(t *testing.T) {
	source := t.TempDir()
	root := t.TempDir()
	contents := []byte("reviewed executable")
	if err := os.WriteFile(filepath.Join(source, "provider"), contents, 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	manifest := Manifest{APIVersion: APIVersion, Name: "fixture", Version: "1.0.0", Kind: "sample.provider", Command: "provider", SHA256: hex.EncodeToString(digest[:])}
	encoded, _ := json.Marshal(manifest)
	path := filepath.Join(source, "plugin.json")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	installed, err := Install(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := List(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed.Directory, "provider"), []byte("changed"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := List(root); err == nil {
		t.Fatal("tampered plugin remained loadable")
	}
	if err := Remove(root, "fixture"); err != nil {
		t.Fatal(err)
	}
}

func TestManifestRejectsTraversalAndInvalidKinds(t *testing.T) {
	m := Manifest{APIVersion: APIVersion, Name: "fixture", Version: "1.0.0", Kind: "sample.provider", Command: "../provider", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if m.Validate() == nil {
		t.Fatal("accepted command traversal")
	}
	m.Command = "provider"
	m.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	m.Kind = "other"
	if m.Validate() == nil {
		t.Fatal("accepted undotted plugin kind")
	}
	m.Kind = "another.provider"
	if err := m.Validate(); err != nil {
		t.Fatalf("rejected a library-defined plugin kind: %v", err)
	}
	m.APIVersion = legacyAPIVersion
	if err := m.Validate(); err != nil {
		t.Fatalf("rejected an installed legacy protocol version: %v", err)
	}
	m.APIVersion = APIVersion
	m.Command, m.SHA256 = "", ""
	m.Files = []PackageFile{{Path: "..", SHA256: strings.Repeat("a", 64)}}
	if err := m.Validate(); err == nil {
		t.Fatal("accepted package file traversal")
	}
}

func TestManifestSupportsVersionedContracts(t *testing.T) {
	m := Manifest{
		APIVersion: APIVersion, Name: "fixture", Version: "1.0.0",
		Contracts: []Contract{{Name: "blockade.provider", Version: "v1alpha1"}},
		Command:   "provider", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if !m.Supports("blockade.provider", "v1alpha1") || m.Supports("board.provider", "v1alpha1") {
		t.Fatalf("contract matching failed: %#v", m.Contracts)
	}
	m.Kind = "blockade.provider"
	if err := m.Validate(); err == nil {
		t.Fatal("accepted ambiguous contracts and legacy kind")
	}
}

func TestInstallSourcePackageAndRejectsResourceTampering(t *testing.T) {
	source := t.TempDir()
	root := t.TempDir()
	files := map[string][]byte{
		"blockade.json":      []byte(`{"runtime":{"name":"python"}}`),
		"source/provider.py": []byte("print('provider')\n"),
	}
	manifest := Manifest{APIVersion: APIVersion, Name: "source-fixture", Version: "1.0.0", Contracts: []Contract{{Name: "blockade.provider", Version: "v1alpha1"}}}
	for name, content := range files {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(content)
		manifest.Files = append(manifest.Files, PackageFile{Path: name, SHA256: hex.EncodeToString(hash[:])})
	}
	encoded, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(source, "plugin.json")
	if err := os.WriteFile(manifestPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	installed, err := Install(root, manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installed.Resource("source/provider.py"); err != nil {
		t.Fatal(err)
	}
	if _, err := installed.Executable(); err == nil {
		t.Fatal("source package unexpectedly exposed an executable")
	}
	if err := os.WriteFile(filepath.Join(installed.Directory, "source", "provider.py"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := List(root); err == nil {
		t.Fatal("tampered source package remained loadable")
	}
}

func TestUpgradeReplacesVersionAndPreservesIdentity(t *testing.T) {
	root := t.TempDir()
	write := func(version string, content []byte) string {
		dir := t.TempDir()
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "provider"), content, 0700); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(content)
		m := Manifest{APIVersion: APIVersion, Name: "fixture", Version: version, Kind: "sample.provider", Command: "provider", SHA256: hex.EncodeToString(hash[:])}
		encoded, _ := json.Marshal(m)
		path := filepath.Join(dir, "plugin.json")
		if err := os.WriteFile(path, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	first := write("1.0.0", []byte("first"))
	if _, err := Install(root, first); err != nil {
		t.Fatal(err)
	}
	second := write("1.1.0", []byte("second"))
	updated, err := Upgrade(root, second)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Manifest.Version != "1.1.0" {
		t.Fatalf("version = %q", updated.Manifest.Version)
	}
	items, err := List(root)
	if err != nil || len(items) != 1 || items[0].Manifest.Version != "1.1.0" {
		t.Fatalf("installed = %#v %v", items, err)
	}
	if _, err := Upgrade(root, second); err == nil {
		t.Fatal("accepted same-version upgrade")
	}
}
