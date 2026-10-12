package blockadeplugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"blockade"
	"github.com/webong/ext/pkg/plugin"
	"github.com/webong/ext/pkg/plugin/packagekit"
	"github.com/webong/ext/pkg/plugin/store"
)

func TestNormalizeProcessErrorPreservesBlockadeFailureKind(t *testing.T) {
	err := normalizeProcessError(&plugin.RemoteError{
		Code:                   string(blockade.ProviderAdapterErrorRateLimit),
		Message:                "capacity exhausted",
		RetryAfterMilliseconds: 1500,
	})
	var adapterError *blockade.ProviderAdapterError
	if !errors.As(err, &adapterError) {
		t.Fatalf("error = %T %v", err, err)
	}
	if adapterError.Kind != blockade.ProviderAdapterErrorRateLimit || adapterError.RetryAfter != 1500*time.Millisecond {
		t.Fatalf("adapter error = %#v", adapterError)
	}
}

// A standalone Blockade application can register its own provider packages
// without importing or running Cymonkey.
func TestRegisterInstalledSelectsOnlyBlockadeProviders(t *testing.T) {
	registry := blockade.NewProviderAdapterRegistry()
	installed := []store.Installed{
		testInstalled(t, "vision", ContractName, Operations),
		testInstalled(t, "camera", "board.provider", []string{"list"}),
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

func TestSourceLaunchPlanAcceptsHostOwnedDescriptor(t *testing.T) {
	source := t.TempDir()
	descriptor := []byte(`{"apiVersion":"host.content-source/v1alpha1","runtime":{"name":"python","version":"3.12.8"},"entrypoint":"provider.py"}`)
	worker := []byte("print('worker')\n")
	if err := os.WriteFile(filepath.Join(source, "content.json"), descriptor, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "provider.py"), worker, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := packagekit.SourceManifest("source-fixture", "1.0.0",
		[]plugin.Contract{testContract("host.content", Operations)}, source, []string{"content.json", "provider.py"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteManifest(source, manifest); err != nil {
		t.Fatal(err)
	}
	packages, err := store.New(t.TempDir(), store.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := packages.Install(source)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := newLaunchPlan(installed, Protocol{DescriptorFile: "content.json", DescriptorAPIVersion: "host.content-source/v1alpha1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.descriptor == nil || plan.descriptor.Entrypoint != "provider.py" {
		t.Fatalf("launch plan = %#v", plan)
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
	descriptor := []byte(`{"apiVersion":"blockade.provider-package/v1alpha1","runtime":{"name":"python","version":"3.12.8"},"entrypoint":"python/provider.py"}`)
	provider := []byte(`
import base64, json, os, sys
with open(os.environ["BLOCKADE_PLUGIN_MANIFEST"], encoding="utf-8") as source:
    package = json.load(source)
ready = False
for line in sys.stdin:
    request = json.loads(line)
    operation = request["operation"]
    payload = request.get("payload")
    if operation == "plugin.hello": result = package["descriptor"]
    elif operation == "start": ready = True; result = {}
    elif operation == "capabilities": result = {"apiVersion":"blockade.provider-adapter/v1alpha1","capabilities":["image.observe"]}
    elif operation == "health": result = {"apiVersion":"blockade.provider-adapter/v1alpha1","ready":ready}
    elif operation == "observe":
        inner = payload["request"]["request"]
        base64.b64decode(inner["image"])
        result = {"apiVersion":"blockade.provider-adapter/v1alpha1","response":{"apiVersion":"blockade.observation/v1alpha1","requestId":inner["requestId"],"observations":[{"kind":"text","label":str(os.getpid()),"confidence":1,"region":{"x":0,"y":0,"width":1,"height":1}}]}}
    elif operation == "close": ready = False; result = {}
    else: raise RuntimeError(operation)
    print(json.dumps({"apiVersion":"ext.plugin/v1","id":request["id"],"payload":result}, separators=(",",":")), flush=True)
`)
	if err := os.WriteFile(filepath.Join(providerSource, "blockade.json"), descriptor, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(providerSource, "python"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providerSource, "python", "provider.py"), provider, 0600); err != nil {
		t.Fatal(err)
	}
	pluginManifest, err := packagekit.SourceManifest("source-vision", "1.0.0",
		[]plugin.Contract{testContract(ContractName, Operations)}, providerSource, []string{"blockade.json", "python"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteManifest(providerSource, pluginManifest); err != nil {
		t.Fatal(err)
	}
	packages, err := store.New(t.TempDir(), store.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := packages.Install(providerSource)
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

func testContract(name string, operations []string) plugin.Contract {
	declared := make([]plugin.Operation, 0, len(operations))
	for _, operation := range operations {
		declared = append(declared, plugin.Operation{Name: operation})
	}
	return plugin.Contract{ContractRef: plugin.ContractRef{Name: name, Version: ContractVersion}, Operations: declared}
}

// testInstalled builds a verified executable package without running it.
func testInstalled(t *testing.T, name, contract string, operations []string) store.Installed {
	t.Helper()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "provider"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, err := packagekit.ExecutableManifest(name, "1.0.0", []plugin.Contract{testContract(contract, operations)}, filepath.Join(source, "provider"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteManifest(source, manifest); err != nil {
		t.Fatal(err)
	}
	packages, err := store.New(t.TempDir(), store.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := packages.Install(source)
	if err != nil {
		t.Fatal(err)
	}
	return installed
}
