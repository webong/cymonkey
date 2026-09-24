package jangolova

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	contract "jangolova/contract"
	"jangolova/sdk"
)

const fixtureExtensionID = "abcdefghijklmnopabcdefghijklmnop"

func TestAdapterDefaultsToNoInstallCDPAndDisconnects(t *testing.T) {
	worker, err := filepath.Abs("../../tests/cymonkey-worker-fixture.mjs")
	if err != nil {
		t.Fatal(err)
	}
	options, _ := json.Marshal(map[string]string{"workerPath": worker})
	connected, err := (Adapter{Host: testHost()}).Connect(context.Background(), sdk.EngineSpec{Options: options}, sdk.EngineTarget{
		Kind: "browser",
		Endpoints: []sdk.TargetEndpoint{{
			Name: "control", Protocol: "cdp", URL: "wss://browser.remote.example/devtools/browser/42",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	healthCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	health := connected.(sdk.EngineHealthProvider).EngineHealth(healthCtx)
	cancel()
	if health.Status != sdk.EngineHealthHealthy {
		t.Fatalf("EngineHealth() = %#v", health)
	}
	if !contains(connected.(sdk.EngineCapabilityProvider).EngineCapabilities(), "script.register") {
		t.Fatal("worker capability was not retained")
	}
	if err := connected.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterRequiresCallerOwnedCompatibleBrowserTarget(t *testing.T) {
	t.Parallel()
	adapter := Adapter{Host: testHost()}
	for name, fixture := range map[string]struct {
		spec   sdk.EngineSpec
		target sdk.EngineTarget
	}{
		"wrong kind":  {target: sdk.EngineTarget{Kind: "native"}},
		"missing all": {target: sdk.EngineTarget{Kind: "browser"}},
		"invalid cdp": {target: sdk.EngineTarget{Kind: "browser", Endpoints: []sdk.TargetEndpoint{{Protocol: "cdp", URL: "file:///browser"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := adapter.Connect(context.Background(), fixture.spec, fixture.target); err == nil {
				t.Fatal("Connect() error = nil")
			}
		})
	}
}

func TestDecodeOptionsRejectsUnknownAndInvalidValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		`{"extensionId":"abcdefghijklmnopabcdefghijklmnop","browserExecutable":"chromium"}`,
		`{"extension":{"mode":"required"}}`,
		`{"extension":{"id":"not-an-extension"}}`,
		`{"driver":"invalid driver"}`,
		`{"backend":"bidi"}`,
	} {
		if _, err := decodeOptions(json.RawMessage(value)); err == nil {
			t.Fatalf("decodeOptions(%s) error = nil", value)
		}
	}
}

func TestDecodeOptionsDefaultsToAutoAndAcceptsNoExtension(t *testing.T) {
	config, err := decodeOptions(json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if config.Domain != "" || config.Driver != "auto" || config.Extension.Mode != extensionAuto || config.Extension.ID != "" {
		t.Fatalf("decodeOptions() = %#v", config)
	}
}

func TestRuntimeDomainIsInferredFromCallerOwnedTarget(t *testing.T) {
	for name, fixture := range map[string]struct {
		requested contract.Domain
		target    sdk.EngineTarget
		want      contract.Domain
	}{
		"browser viewer":  {target: sdk.EngineTarget{Kind: "browser"}, want: contract.DomainViewer},
		"macos viewer":    {target: sdk.EngineTarget{Kind: "macos-application"}, want: contract.DomainViewer},
		"native render":   {target: sdk.EngineTarget{Kind: "godot"}, want: contract.DomainRender},
		"explicit viewer": {requested: contract.DomainViewer, target: sdk.EngineTarget{Kind: "browser"}, want: contract.DomainViewer},
		"browser render":  {requested: contract.DomainRender, target: sdk.EngineTarget{Kind: "browser"}, want: contract.DomainRender},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := resolveDomain(fixture.requested, fixture.target)
			if err != nil || got != fixture.want {
				t.Fatalf("resolveDomain() = %q, %v", got, err)
			}
		})
	}
}

func TestViewerDomainReturnsCallerOwnedNativeHelperLaunchMaterial(t *testing.T) {
	connected, err := (Adapter{Host: testHost()}).Connect(context.Background(), sdk.EngineSpec{Options: json.RawMessage(`{"domain":"viewer"}`)}, sdk.EngineTarget{Kind: "macos-application"})
	if err != nil {
		t.Fatal(err)
	}
	defer connected.Disconnect(context.Background())
	launch, ok := connected.(sdk.EngineCallerLaunchProvider)
	if !ok || !strings.HasPrefix(launch.EngineCallerLaunch().Environment["JANGOLOVA_CYMONKEY_CONTROL_URL"], "ws://127.0.0.1:") {
		t.Fatalf("caller launch = %#v", launch)
	}
}

func TestBackendSelectionPrefersCDPThenBiDiThenSafariMCP(t *testing.T) {
	for name, fixture := range map[string]struct {
		target sdk.EngineTarget
		want   BackendName
	}{
		"cdp":    {target: sdk.EngineTarget{Kind: "browser", Endpoints: []sdk.TargetEndpoint{{Protocol: "webdriver-bidi"}, {Protocol: "cdp"}}}, want: BackendCDP},
		"bidi":   {target: sdk.EngineTarget{Kind: "browser", Endpoints: []sdk.TargetEndpoint{{Protocol: "webdriver-bidi"}}}, want: BackendBiDi},
		"safari": {target: sdk.EngineTarget{Kind: "browser", Endpoints: []sdk.TargetEndpoint{{Protocol: "mcp-streamable-http"}}}, want: BackendSafariMCP},
	} {
		t.Run(name, func(t *testing.T) {
			backend, err := selectBackend("auto", fixture.target)
			if err != nil || backend.Name() != fixture.want {
				t.Fatalf("selectBackend() = %v, %v", backend, err)
			}
		})
	}
}

func TestDriverPresetsSelectCompatibleProtocolBackends(t *testing.T) {
	for name, fixture := range map[string]struct {
		driver string
		target sdk.EngineTarget
		want   BackendName
	}{
		"playwright cdp": {driver: "playwright", target: sdk.EngineTarget{Kind: "browser", Endpoints: []sdk.TargetEndpoint{{Protocol: "cdp"}}}, want: BackendCDP},
		"puppeteer cdp":  {driver: "puppeteer", target: sdk.EngineTarget{Kind: "browser", Endpoints: []sdk.TargetEndpoint{{Protocol: "cdp"}}}, want: BackendCDP},
		"puppeteer bidi": {driver: "puppeteer", target: sdk.EngineTarget{Kind: "browser", Endpoints: []sdk.TargetEndpoint{{Protocol: "webdriver-bidi"}}}, want: BackendBiDi},
	} {
		t.Run(name, func(t *testing.T) {
			backend, err := selectBackendForDomain(fixture.driver, contract.DomainViewer, fixture.target)
			if err != nil || backend.Name() != fixture.want {
				t.Fatalf("selectBackendForDomain(%q) = %v, %v", fixture.driver, backend, err)
			}
		})
	}
}

func TestBrowserDriversSupportViewerAndRenderDomains(t *testing.T) {
	target := sdk.EngineTarget{Kind: "browser", Endpoints: []sdk.TargetEndpoint{{Protocol: "cdp"}}}
	for _, domain := range []contract.Domain{contract.DomainViewer, contract.DomainRender} {
		backend, err := selectBackendForDomain("auto", domain, target)
		if err != nil || backend.Name() != BackendCDP {
			t.Fatalf("selectBackendForDomain(%q) = %v, %v", domain, backend, err)
		}
	}
}

func TestSafariMapperDoesNotInferAugmentationFromGenericInteractionTools(t *testing.T) {
	discovered := []sdk.Capability{
		{Name: "mcp.tool.click", Effect: sdk.EffectWrite, InputSchema: objectSchema("selector")},
		{Name: "mcp.tool.screenshot", Effect: sdk.EffectRead, InputSchema: objectSchema()},
		{Name: "window.evaluate", Effect: sdk.EffectExternal, InputSchema: objectSchema("expression")},
		{Name: "mcp.tool.add_preload_script", Effect: sdk.EffectExternal, InputSchema: objectSchema("source")},
	}
	_, capabilities := mapSafariCapabilities(discovered, nil)
	if !contains(capabilityNamesFromDescriptors(capabilities), "window.evaluate") || !contains(capabilityNamesFromDescriptors(capabilities), "script.register") {
		t.Fatalf("mapped capabilities = %#v", capabilities)
	}
	if contains(capabilityNamesFromDescriptors(capabilities), "augmentation.install") {
		t.Fatalf("generic tools inferred augmentation support: %#v", capabilities)
	}
}

func TestInspectionFindsRepositoryWorker(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	worker := filepath.Join(filepath.Dir(file), "..", "..", "scripts", "cymonkey-worker.mjs")
	t.Setenv("JANGOLOVA_CYMONKEY_WORKER", worker)
	inspection := (Adapter{Host: testHost()}).InspectEngine(context.Background())
	if !inspection.Available || !contains(inspection.Capabilities, "script.register") {
		t.Fatalf("InspectEngine() = %#v", inspection)
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
