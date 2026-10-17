//go:build liveframe_live

package webpresentation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	pngcodec "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"cymonkey/pkg/liveframe"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	"jangolova/displaykit/bridge"
)

func TestLiveframeCanvasRoundTrip(t *testing.T) {
	canvasBin, err := exec.LookPath("canvas")
	if err != nil {
		t.Skip("Canvas CLI is not installed")
	}
	browserBin := os.Getenv("LIVEFRAME_CHROME_BIN")
	if browserBin == "" {
		t.Skip("set LIVEFRAME_CHROME_BIN to a Chromium/Chrome executable")
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	starter, err := liveframe.InstallStarter(workspace)
	if err != nil {
		t.Fatal(err)
	}
	// Canvas uses a Unix socket beneath its state dir; Go's nested TempDir
	// names can exceed macOS's socket-path limit.
	stateDir, err := os.MkdirTemp("/private/tmp", "lf-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	canvas := func(command string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, canvasBin, append([]string{command}, args...)...)
		cmd.Env = append(os.Environ(), "CANVAS_STATE_DIR="+stateDir)
		return cmd.CombinedOutput()
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		cmd := exec.CommandContext(stopCtx, canvasBin, "stop")
		cmd.Env = append(os.Environ(), "CANVAS_STATE_DIR="+stateDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Logf("stop isolated Canvas session: %v (%s)", err, output)
		}
	})
	if output, err := canvas("start", "--dir", workspace, "--browser-bin", browserBin, "--headless", "--app=false", "--stealth=false", "--json"); err != nil {
		log, _ := os.ReadFile(filepath.Join(stateDir, "daemon.log"))
		t.Fatalf("start isolated Canvas session: %v (%s); daemon log: %s", err, output, log)
	}
	connector := liveframe.Connector{CanvasPath: canvasBin, StateDir: stateDir}
	session, err := connector.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Liveframe integration test source")
	}
	t.Setenv("CYMONKEY_PRESENTATION_WORKER", filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "scripts", "presentation-worker.mjs"))
	sdkTarget := session.Target()
	sdkSpec := session.PresentationSpec()
	target := orchestrator.EngineTarget{
		APIVersion: sdkTarget.APIVersion, TargetID: sdkTarget.TargetID,
		Kind: sdkTarget.Kind, Metadata: sdkTarget.Metadata,
		Endpoints: []orchestrator.TargetEndpoint{{
			Name: sdkTarget.Endpoints[0].Name, Protocol: sdkTarget.Endpoints[0].Protocol, URL: sdkTarget.Endpoints[0].URL,
		}},
	}
	spec := manifest.EngineSpec{Adapter: sdkSpec.Adapter, RequiredCapabilities: sdkSpec.RequiredCapabilities, Source: sdkSpec.Source, Options: sdkSpec.Options}
	instance, err := (Adapter{}).Connect(ctx, spec, target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := instance.Disconnect(context.Background()); err != nil {
			t.Errorf("detach web-presentation: %v", err)
		}
	}()
	caller := instance.(bridge.Caller)
	if _, err := bridge.ValidateConformance(ctx, caller, bridge.ConformanceOptions{}); err != nil {
		t.Fatalf("Liveframe bridge conformance: %v", err)
	}
	write, _ := json.Marshal(map[string]any{"name": "presentation.write", "input": map[string]any{
		"expectedStateRevision": "0", "html": `<h1 id="proof">Liveframe is live</h1>`,
		"css": "#proof { color: rgb(30, 220, 120); }",
	}})
	if result, err := caller.Call(ctx, bridge.MethodAct, write); err != nil || !bytes.Contains(result, []byte(`"stateRevision":"1"`)) {
		t.Fatalf("presentation.write = %s, %v", result, err)
	}
	style, err := caller.Call(ctx, bridge.MethodAct, json.RawMessage(`{"name":"presentation.execute","input":{"code":"return getComputedStyle(document.querySelector('#proof')).color"}}`))
	if err != nil || !bytes.Contains(style, []byte("rgb(30, 220, 120)")) {
		t.Fatalf("rendered style = %s, %v", style, err)
	}
	capture, _ := json.Marshal(map[string]any{"name": "presentation.capture", "input": map[string]any{}})
	imageResult, err := caller.Call(ctx, bridge.MethodAct, capture)
	if err != nil {
		t.Fatalf("presentation.capture: %v", err)
	}
	var captured struct {
		PNGBase64 string `json:"pngBase64"`
	}
	if err := json.Unmarshal(imageResult, &captured); err != nil {
		t.Fatal(err)
	}
	imageBytes, err := base64.StdEncoding.DecodeString(captured.PNGBase64)
	if err != nil || !bytes.HasPrefix(imageBytes, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("capture is not a PNG: %v", err)
	}
	imageInfo, err := pngcodec.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil || imageInfo.Width < 1 || imageInfo.Height < 1 {
		t.Fatalf("capture has no viewport: %+v, %v", imageInfo, err)
	}
	page, err := os.ReadFile(starter)
	if err != nil {
		t.Fatal(err)
	}
	page = bytes.Replace(page, []byte("<title>Liveframe</title>"), []byte("<title>Liveframe Reloaded</title>"), 1)
	if err := os.WriteFile(starter, page, 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	reloaded := false
	for time.Now().Before(deadline) {
		result, callErr := caller.Call(ctx, bridge.MethodAct, json.RawMessage(`{"name":"presentation.execute","input":{"code":"return document.title"}}`))
		if callErr == nil && strings.Contains(string(result), "Liveframe Reloaded") {
			reloaded = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !reloaded {
		t.Fatal("Canvas did not auto-reload the edited Liveframe page")
	}
	if err := instance.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	current, changed, err := connector.Refresh(ctx, session)
	if err != nil || changed || current.Generation != session.Generation {
		t.Fatalf("Canvas stopped or changed on detach: %+v, %v, %v", current, changed, err)
	}
}
