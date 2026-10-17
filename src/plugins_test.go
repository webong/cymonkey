package main

import (
	"bytes"
	"console/contentengine"
	"context"
	"encoding/json"
	"errors"
	"io"
	"jangolova/contentkit"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"cymonkey/src/internal/bridge"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"jangolova/devicekit"
)

func TestExecutablePluginsAcrossLibraries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(root, "installed"))
	fixture := filepath.Join(root, "fixture")
	build := exec.Command("go", "build", "-o", fixture, "./tests/plugin-fixture")
	build.Dir = filepath.Dir(filepath.Dir(fixtureSourcePath(t)))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build plugin fixture: %v\n%s", err, output)
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name, contract string
	}{
		{"display-fixture", displayPluginContract},
		{"vision-fixture", contentPluginContract},
		{"content-fixture", contentPluginContract},
		{"device-fixture", devicePluginContract},
		{"future-fixture", "future.provider"},
	} {
		source := filepath.Join(root, item.name)
		if err := os.Mkdir(source, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "provider"), data, 0700); err != nil {
			t.Fatal(err)
		}
		contract := pluginContract(item.contract)
		if len(contract.Operations) == 0 {
			contract.Operations = []plugin.Operation{{Name: "noop"}}
		}
		m, err := packagekit.ExecutableManifest(item.name, "1.0.0", []plugin.Contract{contract}, filepath.Join(source, "provider"))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(m)
		manifestPath := filepath.Join(source, store.ManifestName)
		if err := os.WriteFile(manifestPath, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		if err := pluginsCommand([]string{"install", "--manifest", manifestPath}, io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	var listed bytes.Buffer
	if err := pluginsCommand([]string{"list"}, &listed, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(listed.Bytes(), []byte("display-fixture")) || !bytes.Contains(listed.Bytes(), []byte("vision-fixture")) || !bytes.Contains(listed.Bytes(), []byte("content-fixture")) || !bytes.Contains(listed.Bytes(), []byte("device-fixture")) || !bytes.Contains(listed.Bytes(), []byte("future-fixture")) {
		t.Fatalf("installed inventory: %s", listed.String())
	}

	engines, err := engineRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := engines.Engine("future-fixture"); ok {
		t.Fatal("unhandled package contract registered as a display adapter")
	}
	engine, ok := engines.Engine("display-fixture")
	if !ok {
		t.Fatal("display plugin not registered")
	}
	instance, err := engine.Connect(context.Background(), manifest.EngineSpec{Adapter: "display-fixture"}, orchestrator.EngineTarget{Kind: "browser"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := instance.(bridge.Caller).Call(context.Background(), "describe", json.RawMessage(`{}`))
	if err != nil || !bytes.Contains(raw, []byte(`"ok":true`)) {
		t.Fatalf("display plugin call: %s %v", raw, err)
	}
	if capabilities := instance.(orchestrator.EngineCapabilityProvider).EngineCapabilities(); len(capabilities) != 1 || capabilities[0] != "window.screenshot" {
		t.Fatalf("display plugin capabilities = %v", capabilities)
	}
	if health := instance.(orchestrator.EngineHealthProvider).EngineHealth(context.Background()); health.Status != orchestrator.EngineHealthHealthy {
		t.Fatalf("display plugin health = %+v", health)
	}
	decision, err := instance.Authorize(context.Background(), orchestrator.AuthorizeRequest{TargetID: "target", Action: "capture"})
	if err != nil || !decision.Authorized {
		t.Fatalf("display plugin authorize = %+v, %v", decision, err)
	}
	if inspection := engine.(orchestrator.EngineInspector).InspectEngine(context.Background()); !inspection.Available || len(inspection.Capabilities) != 1 {
		t.Fatalf("display plugin inspection = %+v", inspection)
	}
	if err := instance.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}

	vision, err := contentPluginRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := contentengine.InspectConfiguredProviderModel(context.Background(), contentengine.ProviderAdapterConfig{ID: "vision", Provider: "vision-fixture"}, contentkit.InstalledModel{
		Manifest: contentkit.ModelManifest{
			APIVersion: contentkit.ModelManifestAPIVersion, Name: "fixture", Version: "1.0.0", Provider: "vision-fixture",
			Capabilities: []string{contentkit.CapabilityTextInterpret},
			Artifacts:    []contentkit.ModelArtifact{{Role: "weights", File: "weights.gguf", SHA256: strings.Repeat("0", 64)}},
		},
		Directory: t.TempDir(),
	}, vision, contentengine.EnvironmentSecretResolver{}); err != nil {
		t.Fatalf("Blockade plugin model inspection before startup: %v", err)
	}
	backend, err := contentengine.StartConfiguredProviderAdapter(context.Background(), contentengine.ProviderAdapterConfig{ID: "vision", Provider: "vision-fixture"}, vision, contentengine.EnvironmentSecretResolver{})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := backend.Observe(context.Background(), contentkit.ObserveRequest{Image: []byte("image"), RequestID: "request-1"})
	if err != nil {
		t.Fatal(err)
	}
	if observed.RequestID != "request-1" {
		t.Fatalf("Blockade plugin request ID = %q", observed.RequestID)
	}
	interpreter, ok := backend.(contentkit.ContentEngine)
	if !ok {
		t.Fatal("Blockade plugin has no content interpretation interface")
	}
	for _, request := range []contentkit.InterpretRequest{
		{RequestID: "text-1", Parts: []contentkit.ContentPart{{Kind: "text", Text: "Summarize this note"}}},
		{RequestID: "image-1", Parts: []contentkit.ContentPart{{Kind: "image", MediaType: "image/png", Data: []byte("image")}}},
		{RequestID: "audio-1", Parts: []contentkit.ContentPart{{Kind: "audio", MediaType: "audio/wav", Data: []byte("audio")}}},
	} {
		interpreted, err := interpreter.Interpret(context.Background(), request)
		if err != nil || interpreted.RequestID != request.RequestID || len(interpreted.Findings) != 1 {
			t.Fatalf("Blockade plugin interpretation: %#v %v", interpreted, err)
		}
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	contentOnly, err := contentengine.StartConfiguredProviderAdapter(context.Background(), contentengine.ProviderAdapterConfig{ID: "content", Provider: "content-fixture"}, vision, contentengine.EnvironmentSecretResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contentOnly.Observe(context.Background(), contentkit.ObserveRequest{Image: []byte("image")}); !errors.Is(err, contentkit.ErrModalityUnsupported) {
		t.Fatalf("content-only plugin image call = %v", err)
	}
	contentInterpreter := contentOnly.(contentkit.ContentEngine)
	for _, part := range []contentkit.ContentPart{
		{Kind: "text", Text: "Summarize"},
		{Kind: "audio", MediaType: "audio/wav", Data: []byte("sound")},
	} {
		response, err := contentInterpreter.Interpret(context.Background(), contentkit.InterpretRequest{Parts: []contentkit.ContentPart{part}})
		if err != nil || len(response.Findings) != 1 {
			t.Fatalf("content-only plugin %s = %#v %v", part.Kind, response, err)
		}
	}
	if err := contentOnly.Close(); err != nil {
		t.Fatal(err)
	}
	providers, err := devicePluginAdapters()
	if err != nil {
		t.Fatal(err)
	}
	devices, err := devicekit.NewRegistry(providers...)
	if err != nil {
		t.Fatal(err)
	}
	listedDevices, err := devices.List(context.Background())
	if err != nil || len(listedDevices) != 1 {
		t.Fatalf("Board plugin devices: %#v %v", listedDevices, err)
	}
	connection, err := devices.Open(context.Background(), devicekit.OpenRequest{ProviderID: "device-fixture", DeviceID: "device", Grant: devicekit.Grant{Capabilities: []devicekit.Capability{devicekit.DriveRead}, ResourceIDs: []string{"root"}}})
	if err != nil {
		t.Fatal(err)
	}
	action := devicekit.Action{Capability: devicekit.DriveRead, ResourceID: "root", Input: json.RawMessage(`{}`)}
	if _, err := connection.Stream(context.Background(), devicekit.Action{Capability: devicekit.DriveRead, ResourceID: "other"}); err == nil {
		t.Fatal("Board plugin bypassed resource grant")
	}
	content, err := connection.Stream(context.Background(), action)
	if err != nil {
		t.Fatal(err)
	}
	read, err := io.ReadAll(content)
	if err != nil || string(read) != "content" {
		t.Fatalf("Board plugin stream: %q %v", read, err)
	}
	if err := content.Close(); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	var boardOutput bytes.Buffer
	if err := runDevice([]string{"adapter-stream", "--adapter", "device-fixture", "--device", "device", "--capability", "drive.read", "--resource", "root"}, &boardOutput, io.Discard); err != nil {
		t.Fatal(err)
	}
	if boardOutput.String() != "content" {
		t.Fatalf("Board CLI plugin stream = %q", boardOutput.String())
	}
	operator, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close(context.Background())
	operatorDevices := operatorTestRequest(operator, "secret", http.MethodGet, "/v1/devices", "")
	if operatorDevices.Code != http.StatusOK || !bytes.Contains(operatorDevices.Body.Bytes(), []byte(`"adapterId":"device-fixture"`)) {
		t.Fatalf("Board operator devices = %d %s", operatorDevices.Code, operatorDevices.Body.String())
	}
	operatorStream := operatorTestRequest(operator, "secret", http.MethodPost, "/v1/device-streams", `{"open":{"adapterId":"device-fixture","deviceId":"device","grant":{"capabilities":["drive.read"],"resourceIds":["root"]}},"action":{"capability":"drive.read","resourceId":"root","input":{}}}`)
	if operatorStream.Code != http.StatusOK || operatorStream.Body.String() != "content" {
		t.Fatalf("Board operator stream = %d %q", operatorStream.Code, operatorStream.Body.String())
	}
	if err := pluginsCommand([]string{"remove", "device-fixture"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneOnnxProviderUsesExecutableBoundary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(root, "installed"))
	source := filepath.Join(root, "onnx")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(source, "provider")
	build := exec.Command("go", "build", "-o", executable, "./pkg/content-onnx")
	build.Dir = filepath.Dir(filepath.Dir(fixtureSourcePath(t)))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ONNX provider: %v\n%s", err, output)
	}
	pluginManifest, err := packagekit.ExecutableManifest("onnx", "1.0.0",
		[]plugin.Contract{pluginContract(contentPluginContract)}, executable)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(pluginManifest)
	manifestPath := filepath.Join(source, store.ManifestName)
	if err := os.WriteFile(manifestPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := pluginsCommand([]string{"install", "--manifest", manifestPath}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	registry, err := contentPluginRegistry()
	if err != nil {
		t.Fatal(err)
	}
	model := contentkit.InstalledModel{Manifest: contentkit.ModelManifest{
		APIVersion: contentkit.ModelManifestAPIVersion, Name: "detector", Version: "1.0.0", Provider: "onnx",
		Capabilities: []string{contentkit.CapabilityObjectDetect},
		Artifacts:    []contentkit.ModelArtifact{{Role: "yolo", File: "detector.gguf", SHA256: strings.Repeat("0", 64)}},
	}, Directory: t.TempDir()}
	err = contentengine.InspectConfiguredProviderModel(context.Background(), contentengine.ProviderAdapterConfig{ID: "onnx", Provider: "onnx"}, model, registry, nil)
	if kind, ok := contentkit.ProviderAdapterErrorKindOf(err); !ok || kind != contentkit.ProviderAdapterErrorInvalidRequest {
		t.Fatalf("standalone ONNX provider inspection error = %v, kind = %q", err, kind)
	}
}

func fixtureSourcePath(t *testing.T) string {
	t.Helper()
	_, path, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve fixture source")
	}
	return path
}

func TestSourcePackageManifestInstallsEveryListedFile(t *testing.T) {
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(t.TempDir(), "installed"))
	root := t.TempDir()
	staged := filepath.Join(t.TempDir(), "package")
	for name, content := range map[string]string{
		"content.json":                 `{"apiVersion":"x"}`,
		"python/provider.py":           "print('provider')\n",
		"python/lib/helper.py":         "VALUE = 1\n",
		"python/__pycache__/cache.pyc": "ignored",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "python", "run.sh"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "python", "provider.py"), filepath.Join(root, "python", "linked.py")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := pluginsCommand([]string{"manifest", "--type", "content", "--name", "source-fixture", "--version", "1.0.0", "--root", root, "--file", "content.json", "--dir", "python", "--stage", staged}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(staged, store.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	// The python directory is one tree artifact, not one entry per file.
	for _, want := range []string{`"path": "content.json"`, `"path": "python"`, `"kind": "tree"`} {
		if !strings.Contains(string(manifest), want) {
			t.Fatalf("manifest lacks %s:\n%s", want, manifest)
		}
	}
	if strings.Contains(string(manifest), "provider.py") {
		t.Fatalf("tree contents were listed individually:\n%s", manifest)
	}
	if strings.Contains(string(manifest), "pycache") {
		t.Fatal("bytecode cache was included")
	}
	if info, err := os.Lstat(filepath.Join(staged, "python", "linked.py")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("a symlink was not materialized: %v %v", info, err)
	}
	if info, err := os.Stat(filepath.Join(staged, "python", "run.sh")); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("the owner-execute bit was lost: %v %v", info, err)
	}
	if err := pluginsCommand([]string{"install", "--manifest", staged}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	var listed bytes.Buffer
	if err := pluginsCommand([]string{"list"}, &listed, io.Discard); err != nil || !strings.Contains(listed.String(), "source-fixture") {
		t.Fatalf("list = %s %v", listed.String(), err)
	}
	if err := pluginsCommand([]string{"manifest", "--type", "content", "--name", "x", "--version", "1", "--command", "/bin/sh", "--root", root}, io.Discard, io.Discard); err == nil {
		t.Fatal("--command and --root were accepted together")
	}
}

func writeTestPackage(t *testing.T, name string) string {
	t.Helper()
	source := t.TempDir()
	executable := filepath.Join(source, "provider")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n# "+name+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, err := packagekit.ExecutableManifest(name, "1.0.0", []plugin.Contract{pluginContract(contentPluginContract)}, executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteManifest(source, manifest); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestInstalledPluginsReusesTheVerifiedListUntilTheStoreChanges(t *testing.T) {
	root := filepath.Join(t.TempDir(), "installed")
	t.Setenv("CYMONKEY_PLUGIN_DIR", root)
	forgetInstalledPlugins()
	if err := pluginsCommand([]string{"install", "--manifest", writeTestPackage(t, "first")}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	listed, err := installedPlugins()
	if err != nil || len(listed) != 1 {
		t.Fatalf("first list = %d %v", len(listed), err)
	}
	// A package added behind Cymonkey's back is not seen until the reuse window ends.
	other, err := store.New(root, store.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Install(writeTestPackage(t, "second")); err != nil {
		t.Fatal(err)
	}
	if reused, _ := installedPlugins(); len(reused) != 1 {
		t.Fatalf("the verified list was not reused: %d packages", len(reused))
	}
	// Changing the store through Cymonkey drops the reused list at once.
	if err := pluginsCommand([]string{"install", "--manifest", writeTestPackage(t, "third")}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if fresh, _ := installedPlugins(); len(fresh) != 3 {
		t.Fatalf("the list was stale after an install: %d packages", len(fresh))
	}
	forgetInstalledPlugins()
}

func TestStandaloneBoardProviderServesTheInventoryAndKeyboard(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(root, "installed"))
	source := filepath.Join(root, "board")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(source, "provider")
	build := exec.Command("go", "build", "-o", executable, "./pkg/board/cmd/provider")
	build.Dir = filepath.Dir(filepath.Dir(fixtureSourcePath(t)))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Board provider: %v\n%s", err, output)
	}
	pluginManifest, err := packagekit.ExecutableManifest("board", "1.0.0",
		[]plugin.Contract{pluginContract(devicePluginContract)}, executable)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(pluginManifest)
	manifestPath := filepath.Join(source, store.ManifestName)
	if err := os.WriteFile(manifestPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := pluginsCommand([]string{"install", "--manifest", manifestPath}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	drive := t.TempDir()
	if err := os.WriteFile(filepath.Join(drive, "note.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	inventory := filepath.Join(root, "devices.json")
	config := `{"devices":[{"id":"vault","storage":{"path":` + strconv.Quote(drive) + `,"resourceId":"vault"}}]}`
	if err := os.WriteFile(inventory, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CYMONKEY_DEVICE_CONFIG", inventory)

	providers, err := devicePluginAdapters()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := devicekit.NewRegistry(providers...)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := registry.List(context.Background())
	if err != nil || len(devices) == 0 {
		t.Fatalf("Board provider devices: %#v %v", devices, err)
	}
	for _, device := range devices {
		if device.ProviderID == "board" {
			t.Fatalf("a device kept the package name as its provider ID: %#v", device)
		}
	}

	// The keyboard needs Accessibility permission or macOS; either way the
	// request must reach the plugin rather than fail because none is installed.
	if _, err := keyboardPluginProvider(os.Getpid()); err != nil && strings.Contains(err.Error(), "no installed device plugin") {
		t.Fatalf("keyboard provider did not reach the installed plugin: %v", err)
	}
}
