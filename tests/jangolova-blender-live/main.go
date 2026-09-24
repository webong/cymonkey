// This executable is a caller-owned integration fixture, not a runtime service.
// It provisions its own container and exercises the public SDK against Blender.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	jangolova "cymonkey/lib/jangolova"
	"cymonkey/lib/jangolova/registry"
	"cymonkey/lib/jangolova/sdk"
	"github.com/gorilla/websocket"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	root := flag.String("root", ".", "repository fixture root")
	image := flag.String("image", "jangolova/blender-sdk-fixture:debian13", "local Blender image")
	output := flag.String("output", "", "new artifact directory (default temporary)")
	flag.Parse()
	base, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	out := *output
	if out == "" {
		out, err = os.MkdirTemp("", "jangolova-blender-live-")
	} else {
		out, err = filepath.Abs(out)
		if err == nil {
			err = os.Mkdir(out, 0o755)
		}
	}
	if err != nil {
		return err
	}
	fmt.Println("Artifacts:", out)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	code, err := os.ReadFile(filepath.Join(base, "pkg/blender/blender_cymonkey.py"))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(code)
	digest := hex.EncodeToString(sum[:])
	module := registry.Module{ID: "render/blender", Version: "0.1.0", Runtime: "blender", ProtocolVersion: jangolova.ProtocolVersion, Status: "available", Platforms: []string{"linux-amd64"}, Actions: []string{"resource.describe", "object.transform.set", "render.frame"}}
	var index registry.Registry
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			_ = json.NewEncoder(w).Encode(index)
			return
		}
		if r.URL.Path == "/blender_cymonkey.py" {
			_, _ = w.Write(code)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	module.Artifacts = []registry.Artifact{{Platform: "linux-amd64", URL: server.URL + "/blender_cymonkey.py", SHA256: digest, Size: int64(len(code)), MediaType: "text/x-python"}}
	index = registry.Registry{SchemaVersion: registry.SchemaVersion, RegistryID: "local-live-fixture", Modules: []registry.Module{module}}
	discovered, err := registry.Discover(ctx, server.URL+"/index.json", registry.DiscoveryOptions{})
	if err != nil {
		return err
	}
	selected, err := discovered.Select(module.ID, "linux-amd64")
	if err != nil {
		return err
	}
	cached, err := registry.Pull(ctx, selected, "linux-amd64", filepath.Join(out, "cache"), registry.PullOptions{})
	if err != nil {
		return err
	}
	// Discovery and download must not activate anything.
	mounts := 0
	_, err = registry.Activate(ctx, selected, "linux-amd64", cached, registry.Activation{Mount: func(context.Context, registry.Module, io.Reader) (sdk.EngineInstance, error) {
		mounts++
		return nil, nil
	}})
	if err == nil || mounts != 0 {
		return errors.New("activation without approval was not rejected")
	}
	random := make([]byte, 24)
	if _, err = rand.Read(random); err != nil {
		return err
	}
	token := hex.EncodeToString(random)
	name := "jangolova-blender-sdk-" + hex.EncodeToString(random[:6])
	started := false
	cleanup := func() {
		if started {
			stopCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			log, _ := exec.CommandContext(stopCtx, "docker", "logs", name).CombinedOutput()
			_ = os.WriteFile(filepath.Join(out, "blender.log"), log, 0o644)
			_ = exec.CommandContext(stopCtx, "docker", "stop", "-t", "2", name).Run()
		}
	}
	defer cleanup()
	var endpoint string
	var host sdk.Host
	host.ValidateEndpoint = func(e sdk.TargetEndpoint) error {
		if e.URL != endpoint || e.Protocol != "websocket" {
			return errors.New("fixture endpoint was not approved")
		}
		return nil
	}
	host.DialWebSocket = func(ctx context.Context, e sdk.TargetEndpoint) (*websocket.Conn, error) {
		if err := host.Validate(e); err != nil {
			return nil, err
		}
		c, response, err := websocket.DefaultDialer.DialContext(ctx, e.URL, http.Header{"Authorization": []string{"Bearer " + token}})
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if err != nil {
			return nil, errors.New("fixture WebSocket connection failed")
		}
		return c, nil
	}
	connected, err := registry.Activate(ctx, selected, "linux-amd64", cached, registry.Activation{
		Approve: func(_ context.Context, m registry.Module, a registry.Artifact) error {
			if m.ID != module.ID || m.Version != module.Version || a.SHA256 != digest {
				return errors.New("artifact is outside approved fixture scope")
			}
			return nil
		},
		Mount: func(ctx context.Context, m registry.Module, verified io.Reader) (sdk.EngineInstance, error) {
			mounts++
			dir := filepath.Join(out, "module")
			if err := os.Mkdir(dir, 0o755); err != nil {
				return nil, err
			}
			f, err := os.OpenFile(filepath.Join(dir, "blender_cymonkey.py"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if err != nil {
				return nil, err
			}
			_, err = io.Copy(f, verified)
			closeErr := f.Close()
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
			cmd := exec.CommandContext(ctx, "docker", "run", "--detach", "--rm", "--name", name, "--cpus", "4", "--memory", "2g", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--publish", "127.0.0.1::9321", "--mount", "type=bind,src="+dir+",dst=/workspace/module,readonly", "--mount", "type=bind,src="+filepath.Join(base, "tests/blender-cymonkey-fixture")+",dst=/workspace/fixture,readonly", "--mount", "type=bind,src="+out+",dst=/workspace/artifacts", "-e", "JANGOLOVA_CYMONKEY_TOKEN", "-e", "JANGOLOVA_BLENDER_MODULE_DIR=/workspace/module", *image, "--python", "/workspace/fixture/live.py")
			cmd.Env = append(os.Environ(), "JANGOLOVA_CYMONKEY_TOKEN="+token)
			if result, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("start Blender fixture: %w: %s", err, result)
			}
			started = true
			result, err := exec.CommandContext(ctx, "docker", "port", name, "9321/tcp").Output()
			if err != nil {
				return nil, err
			}
			endpoint = "ws://" + strings.TrimSpace(string(result))
			var last error
			for attempts := 0; attempts < 100; attempts++ {
				attempt, done := context.WithTimeout(ctx, 2*time.Second)
				i, err := (jangolova.Adapter{Host: host}).Connect(attempt, sdk.EngineSpec{RequiredCapabilities: m.Actions}, sdk.EngineTarget{Kind: "blender", Endpoints: []sdk.TargetEndpoint{{Protocol: "websocket", URL: endpoint}}})
				done()
				if err == nil {
					return i, nil
				}
				last = err
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(100 * time.Millisecond):
				}
			}
			return nil, fmt.Errorf("Blender did not become ready: %w", last)
		},
	})
	if err != nil {
		if started {
			log, _ := exec.Command("docker", "logs", name).CombinedOutput()
			_ = os.WriteFile(filepath.Join(out, "blender.log"), log, 0o644)
		}
		return err
	}
	if mounts != 1 {
		return errors.New("unexpected mount count")
	}
	caller := connected.(sdk.Caller)
	badHost := host
	badHost.DialWebSocket = func(ctx context.Context, e sdk.TargetEndpoint) (*websocket.Conn, error) {
		connection, response, err := websocket.DefaultDialer.DialContext(ctx, e.URL, http.Header{"Authorization": []string{"Bearer invalid-fixture-token"}})
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return connection, err
	}
	unauthorized, rejected := (jangolova.Adapter{Host: badHost}).Connect(ctx, sdk.EngineSpec{}, sdk.EngineTarget{Kind: "blender", Endpoints: []sdk.TargetEndpoint{{Protocol: "websocket", URL: endpoint}}})
	if rejected == nil {
		_ = unauthorized.Disconnect(ctx)
		return errors.New("runtime accepted an invalid authentication token")
	}
	call := func(method string, input any) (json.RawMessage, error) {
		p, _ := json.Marshal(input)
		return caller.Call(ctx, method, p)
	}
	act := func(name string, input map[string]any) (json.RawMessage, error) {
		return call("act", map[string]any{"name": name, "input": input})
	}
	if _, err = act("object.transform.set", map[string]any{"targetId": "object:unregistered", "position": map[string]int{"x": 0, "y": 0, "z": 0}}); err == nil {
		return errors.New("unregistered resource was accepted")
	}
	if _, err = act("object.visibility.set", map[string]any{"targetId": "object:roof", "visible": false}); err == nil {
		return errors.New("resource-specific action deny was not enforced")
	}
	if _, err = call("script.execute", map[string]any{}); err == nil {
		return errors.New("unadvertised method was accepted")
	}
	beforeEvents, err := call("events", map[string]any{"after": "0"})
	if err != nil {
		return err
	}
	var cursor struct {
		Cursor string `json:"cursor"`
	}
	if err = json.Unmarshal(beforeEvents, &cursor); err != nil {
		return err
	}
	if _, err = act("render.frame", map[string]any{"targetId": "scene:fixture", "outputPath": "/workspace/artifacts/before.png"}); err != nil {
		return err
	}
	if _, err = act("object.transform.set", map[string]any{"targetId": "object:roof", "position": map[string]float64{"x": 1.6, "y": 0, "z": 2.2}}); err != nil {
		return err
	}
	if _, err = act("render.frame", map[string]any{"targetId": "scene:fixture", "outputPath": "/workspace/artifacts/after.png"}); err != nil {
		return err
	}
	events, err := call("events", map[string]any{"after": cursor.Cursor})
	if err != nil {
		return err
	}
	if !strings.Contains(string(events), "event:resource-changed") {
		return errors.New("no resource change event was emitted")
	}
	_ = os.WriteFile(filepath.Join(out, "events.json"), events, 0o644)
	a, err := pixels(filepath.Join(out, "before.png"))
	if err != nil {
		return err
	}
	b, err := pixels(filepath.Join(out, "after.png"))
	if err != nil {
		return err
	}
	if a == b {
		return errors.New("semantic mutation did not change rendered pixels")
	}
	if err = connected.Disconnect(ctx); err != nil {
		return err
	}
	state, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", name).Output()
	if err != nil || strings.TrimSpace(string(state)) != "true" {
		return errors.New("detaching stopped the caller-owned Blender runtime")
	}
	again, err := (jangolova.Adapter{Host: host}).Connect(ctx, sdk.EngineSpec{}, sdk.EngineTarget{Kind: "blender", Endpoints: []sdk.TargetEndpoint{{Protocol: "websocket", URL: endpoint}}})
	if err != nil {
		return err
	}
	if _, err = again.(sdk.Caller).Call(ctx, "health", json.RawMessage(`{}`)); err != nil {
		return err
	}
	_ = again.Disconnect(ctx)
	log, _ := exec.CommandContext(ctx, "docker", "logs", name).CombinedOutput()
	_ = os.WriteFile(filepath.Join(out, "blender.log"), log, 0o644)
	report := map[string]any{"ok": true, "module": module.ID, "artifactSHA256": digest, "realEngineRendering": true, "beforePixelSHA256": a, "afterPixelSHA256": b, "targetPreserved": true, "checks": []string{"discover", "sha256", "explicit-activation", "hello", "capabilities", "describe", "health", "act", "events", "authentication-deny", "resource-deny", "action-deny", "detach-reconnect"}}
	data, _ := json.MarshalIndent(report, "", "  ")
	if err = os.WriteFile(filepath.Join(out, "report.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
func pixels(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return "", err
	}
	if img.Bounds().Dx() != 384 || img.Bounds().Dy() != 384 {
		return "", errors.New("unexpected rendered image dimensions")
	}
	h := sha256.New()
	colors := map[uint32]struct{}{}
	for y := 0; y < 384; y++ {
		for x := 0; x < 384; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			p := []byte{byte(r >> 8), byte(g >> 8), byte(b >> 8), byte(a >> 8)}
			_, _ = h.Write(p)
			colors[r<<16|g] = struct{}{}
		}
	}
	if len(colors) < 8 {
		return "", errors.New("rendered image is blank or uniform")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
