package blockadeplugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"blockade"
	"providerplugin"
)

// A standalone Blockade application can register its own provider packages
// without importing or running Cymonkey.
func TestRegisterInstalledSelectsOnlyBlockadeProviders(t *testing.T) {
	registry := blockade.NewProviderAdapterRegistry()
	installed := []providerplugin.Installed{
		{Manifest: providerplugin.Manifest{APIVersion: providerplugin.APIVersion, Name: "vision", Version: "1.0.0", Contracts: []providerplugin.Contract{{Name: ContractName, Version: ContractVersion}}, Command: "provider", SHA256: strings.Repeat("0", 64)}},
		{Manifest: providerplugin.Manifest{APIVersion: providerplugin.APIVersion, Name: "camera", Version: "1.0.0", Contracts: []providerplugin.Contract{{Name: "board.provider", Version: "v1alpha1"}}, Command: "provider", SHA256: strings.Repeat("0", 64)}},
	}
	if err := RegisterInstalled(registry, installed); err != nil {
		t.Fatal(err)
	}
	placeholder := func(context.Context, blockade.ProviderAdapterRuntime) (blockade.ProviderAdapter, error) {
		return nil, nil
	}
	if err := registry.Register("vision", placeholder); err == nil {
		t.Fatal("Blockade provider was not registered")
	}
	if err := registry.Register("camera", placeholder); err != nil {
		t.Fatalf("Board provider leaked into Blockade: %v", err)
	}
}

func TestSourceProviderUsesBlockadeManagedRuntimeAndWorkerPool(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable for the managed runtime fixture")
	}
	runtimeSource := t.TempDir()
	runtimeRoot := t.TempDir()
	launcher := []byte("#!/bin/sh\nexec " + strconv.Quote(python) + " \"$@\"\n")
	launcherPath := filepath.Join(runtimeSource, "bin", "python3")
	if err := os.MkdirAll(filepath.Dir(launcherPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcherPath, launcher, 0700); err != nil {
		t.Fatal(err)
	}
	runtimeDigest := sha256.Sum256(launcher)
	runtimeManifest := blockade.RuntimeManifest{
		APIVersion: blockade.RuntimeManifestAPIVersion,
		Name:       "python",
		Version:    "3.12.8",
		Platform:   runtime.GOOS + "-" + runtime.GOARCH,
		Executable: "bin/python3",
		Files:      []blockade.RuntimeFile{{Path: "bin/python3", SHA256: hex.EncodeToString(runtimeDigest[:]), Executable: true}},
	}
	writeJSON(t, filepath.Join(runtimeSource, "runtime.json"), runtimeManifest)
	if _, err := blockade.InstallRuntime(runtimeRoot, filepath.Join(runtimeSource, "runtime.json")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BLOCKADE_RUNTIME_DIR", runtimeRoot)

	providerSource := t.TempDir()
	descriptor := []byte(`{"apiVersion":"blockade.provider-package/v1alpha1","runtime":{"name":"python","version":"3.12.8"},"entrypoint":"provider.py"}`)
	provider := []byte(`
import base64, json, os, sys
with open(os.environ["BLOCKADE_PLUGIN_MANIFEST"], encoding="utf-8") as source:
    manifest = json.load(source)
ready = False
for line in sys.stdin:
    request = json.loads(line)
    method = request["method"]
    if method == "plugin.hello": result = manifest
    elif method == "blockade.start": ready = True; result = {}
    elif method == "blockade.capabilities": result = {"apiVersion":"blockade.provider-adapter/v1alpha1","capabilities":["image.observe"]}
    elif method == "blockade.health": result = {"apiVersion":"blockade.provider-adapter/v1alpha1","ready":ready}
    elif method == "blockade.observe":
        inner = request["params"]["request"]["request"]
        base64.b64decode(inner["image"])
        result = {"apiVersion":"blockade.provider-adapter/v1alpha1","response":{"apiVersion":"blockade.observation/v1alpha1","requestId":inner["requestId"],"observations":[{"kind":"text","label":str(os.getpid()),"confidence":1,"region":{"x":0,"y":0,"width":1,"height":1}}]}}
    elif method == "blockade.close": ready = False; result = {}
    else: raise RuntimeError(method)
    print(json.dumps({"id":request["id"],"result":result}, separators=(",",":")), flush=True)
`)
	if err := os.WriteFile(filepath.Join(providerSource, "blockade.json"), descriptor, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providerSource, "provider.py"), provider, 0600); err != nil {
		t.Fatal(err)
	}
	descriptorDigest := sha256.Sum256(descriptor)
	providerDigest := sha256.Sum256(provider)
	pluginManifest := providerplugin.Manifest{
		APIVersion: providerplugin.APIVersion,
		Name:       "source-vision",
		Version:    "1.0.0",
		Contracts:  []providerplugin.Contract{{Name: ContractName, Version: ContractVersion}},
		Files: []providerplugin.PackageFile{
			{Path: "blockade.json", SHA256: hex.EncodeToString(descriptorDigest[:])},
			{Path: "provider.py", SHA256: hex.EncodeToString(providerDigest[:])},
		},
	}
	writeJSON(t, filepath.Join(providerSource, "plugin.json"), pluginManifest)
	installed, err := providerplugin.Install(t.TempDir(), filepath.Join(providerSource, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	registry := blockade.NewProviderAdapterRegistry()
	if err := Register(registry, installed); err != nil {
		t.Fatal(err)
	}
	engine, err := blockade.StartConfiguredProviderAdapter(context.Background(), blockade.ProviderAdapterConfig{ID: "source", Provider: "source-vision", Workers: 2}, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	labels := map[string]bool{}
	for index := range 2 {
		response, err := engine.Observe(context.Background(), blockade.ObserveRequest{Image: []byte("image"), RequestID: fmt.Sprintf("request-%d", index)})
		if err != nil {
			t.Fatal(err)
		}
		labels[response.Observations[0].Label] = true
	}
	if len(labels) != 2 {
		t.Fatalf("requests did not rotate across Blockade workers: %#v", labels)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
}
