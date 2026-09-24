package effecthousecymonkey

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jangolova "cymonkey/lib/jangolova"
	contract "cymonkey/lib/jangolova/contract"
	"cymonkey/lib/jangolova/sdk"
)

const originalScript = `@component()
export class TapCounter extends APJS.BasicScriptComponent {
  onStart(): void { console.log("ready"); }
}
`

const replacementScript = `@component()
export class TapCounter extends APJS.BasicScriptComponent {
  onStart(): void { console.log("updated"); }
}
`

func TestEffectHouseProjectBridgeUsesExplicitScriptsAndHashCheckedRollback(t *testing.T) {
	root := t.TempDir()
	scripts := filepath.Join(root, "scripts")
	if err := os.Mkdir(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "TapCounter.ts"), []byte(originalScript), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "ReadOnly.ts"), []byte(originalScript), 0o644); err != nil {
		t.Fatal(err)
	}

	validated := 0
	host := sdk.Host{ValidateEndpoint: func(endpoint sdk.TargetEndpoint) error {
		validated++
		if endpoint.Protocol != Protocol || endpoint.URL != root {
			t.Errorf("unexpected endpoint: %#v", endpoint)
		}
		return nil
	}}
	options := json.RawMessage(`{"allowWrite":true,"registeredScripts":[{"id":"tap-counter","path":"scripts/TapCounter.ts","writable":true},{"id":"read-only","path":"scripts/ReadOnly.ts"}]}`)
	engine, err := (Backend{}).Connect(context.Background(), sdk.EngineSpec{Options: options, RequiredCapabilities: []string{ActionScriptReplace}}, sdk.EngineTarget{TargetID: "effect-project", Kind: Runtime, Endpoints: []sdk.TargetEndpoint{{Protocol: Protocol, URL: root}}}, jangolova.Options{Host: host})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = engine.Disconnect(context.Background()) })
	runtime := engine.(*instance)

	helloRaw, err := runtime.Call(context.Background(), sdk.MethodHello, nil)
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	var hello contract.Hello
	_ = json.Unmarshal(helloRaw, &hello)
	if err := contract.ValidateHello(hello); err != nil {
		t.Fatalf("hello contract: %v", err)
	}
	capabilitiesRaw, err := runtime.Call(context.Background(), sdk.MethodCapabilities, nil)
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	var capabilities []contract.Capability
	_ = json.Unmarshal(capabilitiesRaw, &capabilities)
	if err := contract.ValidateCapabilities(capabilities); err != nil {
		t.Fatalf("capabilities contract: %v", err)
	}

	descriptionRaw, err := runtime.Call(context.Background(), sdk.MethodDescribe, nil)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	var description contract.Description
	_ = json.Unmarshal(descriptionRaw, &description)
	if len(description.Surfaces) != 3 || description.Surfaces[0].Kind != "project" || description.Surfaces[1].Kind != "script" {
		t.Fatalf("description only exposes explicit resources: %#v", description.Surfaces)
	}

	readRaw, err := runtime.Call(context.Background(), sdk.MethodAct, json.RawMessage(`{"name":"script.read","input":{"scriptId":"tap-counter"}}`))
	if err != nil {
		t.Fatalf("script.read: %v", err)
	}
	var read struct {
		SHA256 string `json:"sha256"`
		Source string `json:"source"`
	}
	_ = json.Unmarshal(readRaw, &read)
	if read.SHA256 != digest([]byte(originalScript)) || read.Source != originalScript {
		t.Fatalf("read result = %#v", read)
	}

	invalidRaw, err := runtime.Call(context.Background(), sdk.MethodAct, json.RawMessage(`{"name":"script.validate","input":{"scriptId":"tap-counter","source":"export class Nope {}"}}`))
	if err != nil || !strings.Contains(string(invalidRaw), `"valid":false`) {
		t.Fatalf("invalid validation = %s, %v", invalidRaw, err)
	}

	wrongHash := strings.Repeat("0", 64)
	wrong := json.RawMessage(`{"name":"script.replace","input":{"scriptId":"tap-counter","expectedSha256":"` + wrongHash + `","source":` + quoteJSON(replacementScript) + `}}`)
	if _, err := runtime.Call(context.Background(), sdk.MethodAct, wrong); err == nil || !strings.Contains(err.Error(), "expectedSha256") {
		t.Fatalf("stale replace error = %v", err)
	}
	assertFile(t, filepath.Join(scripts, "TapCounter.ts"), originalScript)

	replace := json.RawMessage(`{"name":"script.replace","input":{"scriptId":"tap-counter","expectedSha256":"` + read.SHA256 + `","source":` + quoteJSON(replacementScript) + `}}`)
	replacedRaw, err := runtime.Call(context.Background(), sdk.MethodAct, replace)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	assertFile(t, filepath.Join(scripts, "TapCounter.ts"), replacementScript)
	var replaced struct {
		Rollback struct {
			Input json.RawMessage `json:"input"`
		} `json:"rollback"`
	}
	_ = json.Unmarshal(replacedRaw, &replaced)
	rollback := json.RawMessage(`{"name":"script.replace","input":` + string(replaced.Rollback.Input) + `}`)
	if _, err := runtime.Call(context.Background(), sdk.MethodAct, rollback); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	assertFile(t, filepath.Join(scripts, "TapCounter.ts"), originalScript)

	denied := json.RawMessage(`{"name":"script.replace","input":{"scriptId":"read-only","expectedSha256":"` + digest([]byte(originalScript)) + `","source":` + quoteJSON(replacementScript) + `}}`)
	if _, err := runtime.Call(context.Background(), sdk.MethodAct, denied); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("read-only replacement error = %v", err)
	}
	assertFile(t, filepath.Join(scripts, "ReadOnly.ts"), originalScript)

	healthRaw, err := runtime.Call(context.Background(), methodHealth, nil)
	if err != nil || !strings.Contains(string(healthRaw), `"status":"connected"`) {
		t.Fatalf("health = %s, %v", healthRaw, err)
	}
	if validated < 8 {
		t.Fatalf("host endpoint validation calls = %d, want validation before calls", validated)
	}
	eventsRaw, err := runtime.Call(context.Background(), sdk.MethodEvents, json.RawMessage(`{"limit":20}`))
	if err != nil || !strings.Contains(string(eventsRaw), "script.replaced") {
		t.Fatalf("events = %s, %v", eventsRaw, err)
	}
}

func TestEffectHouseBridgeRejectsEscapingOrUnregisteredPaths(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "Outside.ts")
	if err := os.WriteFile(outside, []byte(originalScript), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "Outside.ts")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	options := json.RawMessage(`{"registeredScripts":[{"id":"outside","path":"Outside.ts"}]}`)
	_, err := (Backend{}).Connect(context.Background(), sdk.EngineSpec{Options: options}, sdk.EngineTarget{Kind: Runtime, Endpoints: []sdk.TargetEndpoint{{Protocol: Protocol, URL: root}}}, jangolova.Options{Host: sdk.Host{ValidateEndpoint: func(sdk.TargetEndpoint) error { return nil }}})
	if err == nil || !strings.Contains(err.Error(), "escapes project root") {
		t.Fatalf("symlink escape error = %v", err)
	}
	if err := validateRelativeScriptPath("../TapCounter.ts"); err == nil {
		t.Fatal("relative escape accepted")
	}
	if err := validateRelativeScriptPath("scripts/bad-name.ts"); err == nil {
		t.Fatal("invalid Effect House resource name accepted")
	}
}

func assertFile(t *testing.T, path, expected string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != expected {
		t.Fatalf("file %s = %q, %v", path, contents, err)
	}
}

func quoteJSON(value string) string { raw, _ := json.Marshal(value); return string(raw) }
