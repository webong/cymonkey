package userscripts

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreKeepsSourcePrivateAndReplaysOnlyMatchingTarget(t *testing.T) {
	directory := t.TempDir()
	source := "document.documentElement.dataset.test = 'yes';"
	record := Record{Target: "firefox:profile-one", ID: "example", Name: "Example",
		Matches:        []string{"https://example.com/*"},
		ExcludeMatches: []string{}, Enabled: true, Source: source}
	record.Revision = Revision(record)
	if err := Save(directory, record, false); err != nil {
		t.Fatal(err)
	}
	if err := Save(directory, record, false); err == nil {
		t.Fatal("duplicate installation accepted")
	}
	other, err := List(directory, "firefox:profile-two")
	if err != nil || len(other) != 0 {
		t.Fatalf("other target saw script: %v, %v", other, err)
	}
	loaded, err := Load(directory, record.Target, record.ID)
	if err != nil || loaded.Source != source {
		t.Fatalf("stored source changed: %v, %v", loaded, err)
	}
	path, _ := filePath(directory, record.Target, record.ID)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("record permissions: %v, %v", info, err)
	}
	if _, err := SetEnabled(directory, record.Target, record.ID, false); err != nil {
		t.Fatal(err)
	}
	listed, err := List(directory, record.Target)
	if err != nil || len(listed) != 1 || listed[0].Enabled {
		t.Fatalf("disable not stored: %v, %v", listed, err)
	}
	if err := Remove(directory, record.Target, record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("uninstall retained record: %v", err)
	}
}

func TestStoreRejectsTamperedSourceAndUnsupportedGrant(t *testing.T) {
	directory := t.TempDir()
	source := "// @grant none\nwindow.ok = true;"
	record := Record{Target: "browser", ID: "example", Name: "Example",
		Matches: []string{"https://example.com/*"}, ExcludeMatches: []string{}, Enabled: true, Source: source}
	record.Revision = Revision(record)
	if err := Save(directory, record, false); err != nil {
		t.Fatal(err)
	}
	path, _ := filePath(directory, record.Target, record.ID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(data, []byte("window.ok = true;"), []byte("window.ok = false;"), 1)
	if bytes.Equal(data, changed) {
		t.Fatal("test did not alter the stored source")
	}
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(directory, record.Target, record.ID); err == nil {
		t.Fatal("tampered record loaded")
	}
	record.Source = "// @grant GM_xmlhttpRequest\nwindow.ok = true;"
	record.Revision = Revision(record)
	if err := Validate(record); err == nil {
		t.Fatal("unsupported grant accepted")
	}
	if filepath.Base(path) != "example.json" {
		t.Fatal("record path was not scoped to ID")
	}
}
