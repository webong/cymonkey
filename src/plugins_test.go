package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"blockade"
	blockadeplugin "blockade/plugin"
	"board"
	boardplugin "board/plugin"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	jangolovaplugin "jangolova/plugin"
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
	for _, item := range []struct{ name, kind string }{{"display-fixture", jangolovaplugin.Kind}, {"vision-fixture", blockadeplugin.Kind}, {"device-fixture", boardplugin.Kind}, {"future-fixture", "future.provider"}} {
		source := filepath.Join(root, item.name)
		if err := os.Mkdir(source, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "provider"), data, 0700); err != nil {
			t.Fatal(err)
		}
		m := providerplugin.Manifest{APIVersion: providerplugin.APIVersion, Name: item.name, Version: "1.0.0", Kind: item.kind, Command: "provider", SHA256: hex.EncodeToString(digest[:])}
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
	if !bytes.Contains(listed.Bytes(), []byte("display-fixture")) || !bytes.Contains(listed.Bytes(), []byte("vision-fixture")) || !bytes.Contains(listed.Bytes(), []byte("device-fixture")) || !bytes.Contains(listed.Bytes(), []byte("future-fixture")) {
		t.Fatalf("installed inventory: %s", listed.String())
	}

	engines, err := engineRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := engines.Engine("future-fixture"); ok {
		t.Fatal("unhandled package kind registered as a Jangolova engine")
	}
	engine, ok := engines.Engine("display-fixture")
	if !ok {
		t.Fatal("Jangolova plugin not registered")
	}
	instance, err := engine.Connect(context.Background(), manifest.EngineSpec{Adapter: "display-fixture"}, orchestrator.EngineTarget{Kind: "browser"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := instance.(sdk.Caller).Call(context.Background(), "describe", json.RawMessage(`{}`))
	if err != nil || !bytes.Contains(raw, []byte(`"ok":true`)) {
		t.Fatalf("Jangolova plugin call: %s %v", raw, err)
	}
	if err := instance.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}

	vision := blockade.NewProviderAdapterRegistry()
	if err := registerBlockadePlugins(vision); err != nil {
		t.Fatal(err)
	}
	backend, err := blockade.StartConfiguredProviderAdapter(context.Background(), blockade.ProviderAdapterConfig{ID: "vision", Kind: "vision-fixture"}, vision, blockade.EnvironmentSecretResolver{})
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
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}

	providers, err := boardPluginProviders()
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
	if err := runBoard([]string{"provider-stream", "--provider", "device-fixture", "--device", "device", "--capability", "drive.read", "--resource", "root"}, &boardOutput, io.Discard); err != nil {
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
	operatorDevices := operatorTestRequest(operator, "secret", http.MethodGet, "/v1/board/devices", "")
	if operatorDevices.Code != http.StatusOK || !bytes.Contains(operatorDevices.Body.Bytes(), []byte(`"providerId":"device-fixture"`)) {
		t.Fatalf("Board operator devices = %d %s", operatorDevices.Code, operatorDevices.Body.String())
	}
	operatorStream := operatorTestRequest(operator, "secret", http.MethodPost, "/v1/board/streams", `{"open":{"providerId":"device-fixture","deviceId":"device","grant":{"capabilities":["drive.read"],"resourceIds":["root"]}},"action":{"capability":"drive.read","resourceId":"root","input":{}}}`)
	if operatorStream.Code != http.StatusOK || operatorStream.Body.String() != "content" {
		t.Fatalf("Board operator stream = %d %q", operatorStream.Code, operatorStream.Body.String())
	}
	if err := pluginsCommand([]string{"remove", "device-fixture"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
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
