package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"blockade"
	"board"
	"cymonkey/src/contracts"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	"jangolova/sdk"
	"providerplugin"
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
	digest := sha256.Sum256(data)
	for _, item := range []struct {
		name, contract string
		legacy         bool
	}{
		{"display-fixture", displayPluginContract, false},
		{"vision-fixture", contentPluginContract, false},
		{"content-fixture", contentPluginContract, false},
		{"legacy-vision-fixture", contentPluginContract, true},
		{"device-fixture", devicePluginContract, false},
		{"future-fixture", "future.provider", false},
	} {
		source := filepath.Join(root, item.name)
		if err := os.Mkdir(source, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "provider"), data, 0700); err != nil {
			t.Fatal(err)
		}
		m := providerplugin.Manifest{APIVersion: contracts.PluginManifestVersion, Name: item.name, Version: "1.0.0", Command: "provider", SHA256: hex.EncodeToString(digest[:])}
		if item.legacy {
			m.Kind = item.contract
		} else {
			m.Contracts = []providerplugin.Contract{{Name: item.contract, Version: "v1alpha1"}}
		}
		encoded, _ := json.Marshal(m)
		manifestPath := filepath.Join(source, "plugin.json")
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
	if !bytes.Contains(listed.Bytes(), []byte("display-fixture")) || !bytes.Contains(listed.Bytes(), []byte("vision-fixture")) || !bytes.Contains(listed.Bytes(), []byte("content-fixture")) || !bytes.Contains(listed.Bytes(), []byte("legacy-vision-fixture")) || !bytes.Contains(listed.Bytes(), []byte("device-fixture")) || !bytes.Contains(listed.Bytes(), []byte("future-fixture")) {
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
	raw, err := instance.(sdk.Caller).Call(context.Background(), "describe", json.RawMessage(`{}`))
	if err != nil || !bytes.Contains(raw, []byte(`"ok":true`)) {
		t.Fatalf("display plugin call: %s %v", raw, err)
	}
	if err := instance.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}

	vision, err := contentPluginRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := blockade.InspectConfiguredProviderModel(context.Background(), blockade.ProviderAdapterConfig{ID: "vision", Provider: "vision-fixture"}, blockade.InstalledModel{
		Manifest: blockade.ModelManifest{
			APIVersion: blockade.ModelManifestAPIVersion, Name: "fixture", Version: "1.0.0", Provider: "vision-fixture",
			Capabilities: []string{blockade.CapabilityTextInterpret},
			Artifacts:    []blockade.ModelArtifact{{Role: "weights", File: "weights.gguf", SHA256: strings.Repeat("0", 64)}},
		},
		Directory: t.TempDir(),
	}, vision, blockade.EnvironmentSecretResolver{}); err != nil {
		t.Fatalf("Blockade plugin model inspection before startup: %v", err)
	}
	backend, err := blockade.StartConfiguredProviderAdapter(context.Background(), blockade.ProviderAdapterConfig{ID: "vision", Provider: "vision-fixture"}, vision, blockade.EnvironmentSecretResolver{})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := backend.Observe(context.Background(), blockade.ObserveRequest{Image: []byte("image"), RequestID: "request-1"})
	if err != nil {
		t.Fatal(err)
	}
	if observed.RequestID != "request-1" {
		t.Fatalf("Blockade plugin request ID = %q", observed.RequestID)
	}
	interpreter, ok := backend.(blockade.ContentEngine)
	if !ok {
		t.Fatal("Blockade plugin has no content interpretation interface")
	}
	for _, request := range []blockade.InterpretRequest{
		{RequestID: "text-1", Parts: []blockade.ContentPart{{Kind: "text", Text: "Summarize this note"}}},
		{RequestID: "image-1", Parts: []blockade.ContentPart{{Kind: "image", MediaType: "image/png", Data: []byte("image")}}},
		{RequestID: "audio-1", Parts: []blockade.ContentPart{{Kind: "audio", MediaType: "audio/wav", Data: []byte("audio")}}},
	} {
		interpreted, err := interpreter.Interpret(context.Background(), request)
		if err != nil || interpreted.RequestID != request.RequestID || len(interpreted.Findings) != 1 {
			t.Fatalf("Blockade plugin interpretation: %#v %v", interpreted, err)
		}
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	contentOnly, err := blockade.StartConfiguredProviderAdapter(context.Background(), blockade.ProviderAdapterConfig{ID: "content", Provider: "content-fixture"}, vision, blockade.EnvironmentSecretResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contentOnly.Observe(context.Background(), blockade.ObserveRequest{Image: []byte("image")}); !errors.Is(err, blockade.ErrModalityUnsupported) {
		t.Fatalf("content-only plugin image call = %v", err)
	}
	contentInterpreter := contentOnly.(blockade.ContentEngine)
	for _, part := range []blockade.ContentPart{
		{Kind: "text", Text: "Summarize"},
		{Kind: "audio", MediaType: "audio/wav", Data: []byte("sound")},
	} {
		response, err := contentInterpreter.Interpret(context.Background(), blockade.InterpretRequest{Parts: []blockade.ContentPart{part}})
		if err != nil || len(response.Findings) != 1 {
			t.Fatalf("content-only plugin %s = %#v %v", part.Kind, response, err)
		}
	}
	if err := contentOnly.Close(); err != nil {
		t.Fatal(err)
	}
	legacyImage, err := blockade.StartConfiguredProviderAdapter(context.Background(), blockade.ProviderAdapterConfig{ID: "legacy-vision", Provider: "legacy-vision-fixture"}, vision, blockade.EnvironmentSecretResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacyImage.Observe(context.Background(), blockade.ObserveRequest{Image: []byte("image")}); err != nil {
		t.Fatalf("legacy image plugin observation: %v", err)
	}
	if err := legacyImage.Close(); err != nil {
		t.Fatal(err)
	}

	providers, err := devicePluginAdapters()
	if err != nil {
		t.Fatal(err)
	}
	devices, err := board.NewRegistry(providers...)
	if err != nil {
		t.Fatal(err)
	}
	listedDevices, err := devices.List(context.Background())
	if err != nil || len(listedDevices) != 1 {
		t.Fatalf("Board plugin devices: %#v %v", listedDevices, err)
	}
	connection, err := devices.Open(context.Background(), board.OpenRequest{ProviderID: "device-fixture", DeviceID: "device", Grant: board.Grant{Capabilities: []board.Capability{board.DriveRead}, ResourceIDs: []string{"root"}}})
	if err != nil {
		t.Fatal(err)
	}
	action := board.Action{Capability: board.DriveRead, ResourceID: "root", Input: json.RawMessage(`{}`)}
	if _, err := connection.Stream(context.Background(), board.Action{Capability: board.DriveRead, ResourceID: "other"}); err == nil {
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
	build := exec.Command("go", "build", "-o", executable, "./providers/content-onnx")
	build.Dir = filepath.Dir(filepath.Dir(fixtureSourcePath(t)))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ONNX provider: %v\n%s", err, output)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	pluginManifest := providerplugin.Manifest{
		APIVersion: contracts.PluginManifestVersion, Name: "onnx", Version: "1.0.0",
		Contracts: []providerplugin.Contract{{Name: contentPluginContract, Version: pluginContractVersion}},
		Command:   "provider", SHA256: hex.EncodeToString(digest[:]),
	}
	encoded, _ := json.Marshal(pluginManifest)
	manifestPath := filepath.Join(source, "plugin.json")
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
	model := blockade.InstalledModel{Manifest: blockade.ModelManifest{
		APIVersion: blockade.ModelManifestAPIVersion, Name: "detector", Version: "1.0.0", Provider: "onnx",
		Capabilities: []string{blockade.CapabilityObjectDetect},
		Artifacts:    []blockade.ModelArtifact{{Role: "yolo", File: "detector.gguf", SHA256: strings.Repeat("0", 64)}},
	}, Directory: t.TempDir()}
	err = blockade.InspectConfiguredProviderModel(context.Background(), blockade.ProviderAdapterConfig{ID: "onnx", Provider: "onnx"}, model, registry, nil)
	if kind, ok := blockade.ProviderAdapterErrorKindOf(err); !ok || kind != blockade.ProviderAdapterErrorInvalidRequest {
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
