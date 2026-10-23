package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"cymonkey/src/internal/orchestrator"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"jangolova/displaykit"
)

// installDisplayEngines builds Board's display engine program and installs it
// once for each engine name, the way a user installs it.
func installDisplayEngines(t *testing.T, names ...string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(root, "installed"))
	program := filepath.Join(root, "display")
	buildGuest(t, program, "pkg/board/cmd/display")
	data, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		source := filepath.Join(root, "source-"+name)
		if err := os.Mkdir(source, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "provider"), data, 0o700); err != nil {
			t.Fatal(err)
		}
		manifest, err := packagekit.ExecutableManifest(name, "1.0.0", []plugin.Contract{pluginContract(displayPluginContract)}, filepath.Join(source, "provider"))
		if err != nil {
			t.Fatal(err)
		}
		if manifest, err = manifest.WithReverse([]plugin.Contract{displaykit.HostContract()}); err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(manifest)
		manifestPath := filepath.Join(source, store.ManifestName)
		if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := pluginsCommand([]string{"install", "--manifest", manifestPath}, io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRegistryHasNoStandaloneRenderAdapter(t *testing.T) {
	registry, err := engineRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Engine("render"); ok {
		t.Fatal("standalone render adapter must not be registered; engine targets go through the Cymonkey control plane")
	}
}

func TestRegistryHasNoBuiltInDisplayEngineWithoutThePackage(t *testing.T) {
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(t.TempDir(), "installed"))
	registry, err := engineRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"playwright", "puppeteer", "cymonkey"} {
		if _, ok := registry.Engine(name); ok {
			t.Fatalf("%s is registered without an installed display engine package", name)
		}
	}
}

func TestInstalledDisplayEngineServesTheBrowserAutomationAndCymonkeyNames(t *testing.T) {
	installDisplayEngines(t, "playwright", "puppeteer", "cymonkey")
	registry, err := engineRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"playwright", "puppeteer", "cymonkey"} {
		adapter, ok := registry.Engine(name)
		if !ok {
			t.Fatalf("%s engine is not registered", name)
		}
		inspector, ok := adapter.(orchestrator.EngineInspector)
		if !ok {
			t.Fatalf("%s adapter does not support inspection", name)
		}
		inspection := inspector.InspectEngine(context.Background())
		if !inspection.Available || !hasCapability(inspection.Capabilities, "script.register") {
			t.Fatalf("%s inspection = %#v", name, inspection)
		}
	}
}

func hasCapability(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
