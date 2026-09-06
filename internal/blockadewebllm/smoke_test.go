package blockadewebllm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"cymonkey/internal/blockade"
)

// TestWebLLMRuntimeBootProbe verifies the browser/runtime bridge and pinned ES
// module without downloading a model. The deliberately unknown model must
// produce a browser-reported initialization error rather than a process exit.
func TestWebLLMRuntimeBootProbe(t *testing.T) {
	if os.Getenv("BLOCKADE_WEBLLM_BOOT_PROBE") != "1" {
		t.Skip("set BLOCKADE_WEBLLM_BOOT_PROBE=1 to probe Chromium and the WebLLM module")
	}
	settings := map[string]string{
		"model":          "blockade-intentionally-unknown-model",
		"cacheDirectory": t.TempDir(),
	}
	if value := os.Getenv("BLOCKADE_WEBLLM_BROWSER"); value != "" {
		settings["browserExecutable"] = value
	}
	if value := os.Getenv("BLOCKADE_WEBLLM_MODULE_URL"); value != "" {
		settings["moduleURL"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	provider, err := New(ctx, blockade.ProviderAdapterRuntime{ID: "webllm-boot-probe", Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	adapter := provider.(*Adapter)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		adapter.mu.Lock()
		state, detail := adapter.state, adapter.detail
		adapter.mu.Unlock()
		if state == "error" {
			if strings.Contains(detail, "browser exited") || strings.Contains(detail, "browser stopped") {
				t.Fatalf("runtime page did not report an initialization result: %s", detail)
			}
			t.Logf("runtime bridge reported the expected unknown-model failure: %s", detail)
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("runtime boot probe timed out in state %q: %s", state, detail)
		}
	}
}

// TestWebLLMFixtureSmoke is opt-in because it requires Chromium, WebGPU, and
// several gigabytes of model artifacts on a cold cache.
func TestWebLLMFixtureSmoke(t *testing.T) {
	if os.Getenv("BLOCKADE_WEBLLM_SMOKE") != "1" {
		t.Skip("set BLOCKADE_WEBLLM_SMOKE=1 to run browser-local WebLLM inference")
	}
	settings := map[string]string{
		"model": "Phi-3.5-vision-instruct-q4f16_1-MLC",
	}
	for setting, environment := range map[string]string{
		"browserExecutable": "BLOCKADE_WEBLLM_BROWSER",
		"cacheDirectory":    "BLOCKADE_WEBLLM_CACHE",
		"model":             "BLOCKADE_WEBLLM_MODEL",
		"moduleURL":         "BLOCKADE_WEBLLM_MODULE_URL",
	} {
		if value := os.Getenv(environment); value != "" {
			settings[setting] = value
		}
	}
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancelStartup()
	provider, err := New(startupCtx, blockade.ProviderAdapterRuntime{ID: "webllm-smoke", Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		health, err := provider.Health(startupCtx)
		if err != nil {
			t.Fatal(err)
		}
		if health.Ready {
			break
		}
		adapter := provider.(*Adapter)
		adapter.mu.Lock()
		state := adapter.state
		adapter.mu.Unlock()
		if state == "error" {
			t.Fatalf("WebLLM startup failed: %s", health.Detail)
		}
		select {
		case <-ticker.C:
		case <-startupCtx.Done():
			t.Fatalf("WebLLM did not become ready: %v (last status: %s)", startupCtx.Err(), health.Detail)
		}
	}

	image, err := os.ReadFile("../blockade/testdata/smoke.png")
	if err != nil {
		t.Fatal(err)
	}
	observeCtx, cancelObserve := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelObserve()
	response, err := provider.Observe(observeCtx, blockade.ProviderAdapterObserveRequest{
		APIVersion: blockade.ProviderAdapterAPIVersion,
		Request: blockade.ObserveRequest{
			APIVersion: blockade.APIVersion, RequestID: "webllm-smoke-1",
			Image: image, Prompt: "Describe the most important visible item.",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.APIVersion != blockade.ProviderAdapterAPIVersion {
		t.Fatalf("adapter apiVersion = %q", response.APIVersion)
	}
	if err := blockade.ValidateObserveResponse(response.Response); err != nil {
		t.Fatal(err)
	}
	if len(response.Response.Observations) == 0 {
		t.Fatal("WebLLM returned no observations")
	}
}
