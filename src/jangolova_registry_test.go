package main

import (
	"context"
	"testing"

	"cymonkey/src/internal/orchestrator"
)

func TestRegistryHasNoStandaloneRenderAdapter(t *testing.T) {
	registry, err := engineRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Engine("render"); ok {
		t.Fatal("standalone render adapter must not be registered; engine targets go through the Cymonkey control plane")
	}
}

func TestRegistryPinsBrowserAutomationToCymonkeyDrivers(t *testing.T) {
	registry, err := engineRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"playwright", "puppeteer"} {
		adapter, ok := registry.Engine(name)
		if !ok {
			t.Fatalf("%s control plane is not registered", name)
		}
		if _, ok := adapter.(orchestrator.EngineInspector); !ok {
			t.Fatalf("%s adapter does not support inspection", name)
		}
	}
}

func TestRegistryIncludesProviderVisibleCymonkey(t *testing.T) {
	registry, err := engineRegistry()
	if err != nil {
		t.Fatal(err)
	}
	adapter, ok := registry.Engine("cymonkey")
	if !ok {
		t.Fatal("Cymonkey adapter is not registered")
	}
	inspection := adapter.(orchestrator.EngineInspector).InspectEngine(context.Background())
	if !hasCapability(inspection.Capabilities, "script.register") {
		t.Fatalf("Cymonkey inspection = %#v", inspection)
	}
}

func hasCapability(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
