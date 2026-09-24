package browserautomation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
)

func TestAdapterPassesResolvedHeadersToRemoteCDPWorker(t *testing.T) {
	worker, err := filepath.Abs("../../../tests/connection-material-worker.mjs")
	if err != nil {
		t.Fatal(err)
	}
	options, _ := json.Marshal(map[string]string{"workerPath": worker})
	instance, err := Playwright().Connect(context.Background(), manifest.EngineSpec{Options: options}, orchestrator.EngineTarget{
		Kind: "browser",
		Endpoints: []orchestrator.TargetEndpoint{{
			Name: "control", Protocol: "cdp", URL: "wss://browser.remote.example/devtools/browser/42",
			Connection: &orchestrator.EndpointConnection{
				Headers:   map[string]string{"Authorization": "Bearer fixture-secret"},
				ExpiresAt: time.Now().Add(time.Minute),
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterReconnectsWorkerWhenCredentialRotates(t *testing.T) {
	worker, err := filepath.Abs("../../../tests/connection-material-worker.mjs")
	if err != nil {
		t.Fatal(err)
	}
	options, _ := json.Marshal(map[string]string{"workerPath": worker})
	connection := &orchestrator.EndpointConnection{
		Headers:   map[string]string{"Authorization": "Bearer fixture-secret"},
		ExpiresAt: time.Now().Add(time.Minute),
	}
	connected, err := Playwright().Connect(context.Background(), manifest.EngineSpec{Options: options}, orchestrator.EngineTarget{
		Kind: "browser", Endpoints: []orchestrator.TargetEndpoint{{
			Name: "control", Protocol: "cdp", URL: "wss://browser.remote.example/devtools/browser/42", Connection: connection,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connection.ReplaceCredential(map[string]string{"Authorization": "Bearer rotated-secret"}, time.Now().Add(2*time.Minute))
	events := connected.(orchestrator.EngineEventSource).EngineEvents()
	deadline := time.After(5 * time.Second)
	sawRedactedFailure := false
	for {
		select {
		case event := <-events:
			switch event.Type {
			case "interaction.connection.renewal_failed":
				if strings.Contains(event.Message, "rotated-secret") || !strings.Contains(event.Message, "[REDACTED]") {
					t.Fatalf("renewal failure was not redacted: %#v", event)
				}
				sawRedactedFailure = true
			case "interaction.connection.renewed":
				if !sawRedactedFailure {
					t.Fatal("worker reconnect did not exercise retry path")
				}
				if err := connected.Disconnect(context.Background()); err != nil {
					t.Fatal(err)
				}
				return
			}
		case <-deadline:
			t.Fatal("credential rotation did not reconnect the worker")
		}
	}
}

func TestAdapterReplacesWorkerWhenTLSRotates(t *testing.T) {
	worker, err := filepath.Abs("../../../tests/connection-material-worker.mjs")
	if err != nil {
		t.Fatal(err)
	}
	options, _ := json.Marshal(map[string]string{"workerPath": worker})
	connection := &orchestrator.EndpointConnection{
		Headers: map[string]string{"Authorization": "Bearer fixture-secret"}, ExpiresAt: time.Now().Add(time.Minute),
	}
	connected, err := Playwright().Connect(context.Background(), manifest.EngineSpec{Options: options}, orchestrator.EngineTarget{
		Kind: "browser", Endpoints: []orchestrator.TargetEndpoint{{
			Name: "control", Protocol: "cdp", URL: "wss://browser.remote.example/devtools/browser/42", Connection: connection,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(t.TempDir(), "rotated-ca.pem")
	if err := os.WriteFile(caPath, []byte("fixture CA material"), 0o600); err != nil {
		t.Fatal(err)
	}
	revision := connection.ReplaceTLS(&orchestrator.TLSConnection{CAFile: caPath}, time.Now().Add(2*time.Minute))
	select {
	case event := <-connected.(orchestrator.EngineEventSource).EngineEvents():
		if event.Type != "interaction.connection.renewed" {
			t.Fatalf("TLS rotation event = %#v", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TLS rotation did not replace the worker")
	}
	_, acknowledged := connection.Acknowledgements()
	if acknowledged < revision {
		t.Fatalf("acknowledged revision = %d, want at least %d", acknowledged, revision)
	}
	if err := connected.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterRequiresCallerOwnedBrowserTarget(t *testing.T) {
	t.Parallel()
	adapter := Playwright()
	for name, target := range map[string]orchestrator.EngineTarget{
		"wrong kind":  {Kind: "native"},
		"missing cdp": {Kind: "browser"},
		"invalid cdp": {Kind: "browser", Endpoints: []orchestrator.TargetEndpoint{{Name: "cdp", Protocol: "cdp", URL: "file:///browser"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := adapter.Connect(context.Background(), manifest.EngineSpec{}, target); err == nil {
				t.Fatal("Connect() error = nil")
			}
		})
	}
}

func TestDecodeOptionsRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	if _, err := decodeOptions(json.RawMessage(`{"browserExecutable":"chromium"}`)); err == nil {
		t.Fatal("target-launch option was accepted")
	}
}

func TestPuppeteerAcceptsBiDiWhilePlaywrightRequiresCDP(t *testing.T) {
	t.Parallel()
	target := orchestrator.EngineTarget{
		Kind: "browser",
		Endpoints: []orchestrator.TargetEndpoint{{
			Name: "bidi", Protocol: "webdriver-bidi", URL: "ws://127.0.0.1:9222/session",
		}},
	}
	endpoint, protocol, ok := Puppeteer().targetEndpoint(target)
	if !ok || protocol != "webdriver-bidi" || endpoint.URL == "" {
		t.Fatalf("Puppeteer targetEndpoint() = %#v, %q, %v", endpoint, protocol, ok)
	}
	if _, _, ok := Playwright().targetEndpoint(target); ok {
		t.Fatal("Playwright accepted a WebDriver BiDi-only target")
	}
}

func TestInspectionFindsRepositoryWorker(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	worker := filepath.Join(filepath.Dir(file), "..", "..", "..", "scripts", "browser-worker.mjs")
	t.Setenv("JANGOLOVA_BROWSER_WORKER", worker)
	inspection := Puppeteer().InspectEngine(context.Background())
	if !inspection.Available {
		t.Fatalf("InspectEngine() = %#v", inspection)
	}
}
