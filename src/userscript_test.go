package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUserscriptPrepareInstallAndLifecycle(t *testing.T) {
	store := t.TempDir()
	source := filepath.Join(t.TempDir(), "script.js")
	if err := os.WriteFile(source, []byte("document.documentElement.dataset.ready = 'yes';"), 0600); err != nil {
		t.Fatal(err)
	}
	base := []string{"--store", store, "--target", "chrome:profile-one", "--source", source,
		"--id", "example", "--name", "Example", "--match", "https://example.com/*"}
	var output bytes.Buffer
	if err := run(append([]string{"userscript", "prepare"}, base...), &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var prepared struct {
		Status   string `json:"status"`
		Revision string `json:"revision"`
		Source   string `json:"source"`
	}
	if err := json.Unmarshal(output.Bytes(), &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Status != "prepared" || prepared.Revision == "" || prepared.Source != "" {
		t.Fatalf("prepare exposed source or omitted revision: %+v", prepared)
	}
	if err := run(append(append([]string{"userscript", "install"}, base...), "--revision", "sha256:wrong"), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("install accepted an unreviewed revision")
	}
	broader := append([]string{"userscript", "install", "--store", store, "--target", "chrome:profile-one", "--source", source,
		"--id", "example", "--name", "Example", "--match", "*://*/*", "--revision"}, prepared.Revision)
	if err := run(broader, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("install accepted broader matches under the prepared revision")
	}
	output.Reset()
	if err := run(append(append([]string{"userscript", "install"}, base...), "--revision", prepared.Revision), &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run([]string{"userscript", "disable", "--store", store, "--target", "chrome:profile-one", "--id", "example"}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var disabled struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(output.Bytes(), &disabled); err != nil || disabled.Enabled {
		t.Fatalf("disable failed: %s, %v", output.String(), err)
	}
	output.Reset()
	if err := run([]string{"userscript", "list", "--store", store, "--target", "chrome:profile-one"}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte("dataset.ready")) {
		t.Fatal("list leaked userscript source")
	}
}

func TestUserscriptInstallInfersMetadataAndEndpointTarget(t *testing.T) {
	store := t.TempDir()
	source := filepath.Join(t.TempDir(), "reading-tools.user.js")
	content := "// ==UserScript==\n// @name Reading Tools\n// @match https://example.com/*\n// ==/UserScript==\nglobalThis.ready = true;"
	if err := os.WriteFile(source, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"userscript", "install", "--store", store, "--source", source, "--target", "remote-profile"}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Matches []string `json:"matches"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != "reading-tools" || result.Name != "Reading Tools" || len(result.Matches) != 1 || result.Matches[0] != "https://example.com/*" {
		t.Fatalf("metadata was not inferred: %+v", result)
	}
	first, err := resolveUserscriptTarget("", "cdp=http://127.0.0.1:9222")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveUserscriptTarget("", "cdp=http://127.0.0.1:9222")
	if err != nil || first != second || first == "" {
		t.Fatalf("endpoint target is not stable: %q, %q, %v", first, second, err)
	}
}
