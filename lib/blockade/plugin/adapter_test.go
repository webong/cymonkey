package blockadeplugin

import (
	"context"
	"testing"

	"blockade"
	"providerplugin"
)

// A standalone Blockade application can register its own provider executables
// without importing or running Cymonkey.
func TestRegisterInstalledSelectsOnlyBlockadeProviders(t *testing.T) {
	registry := blockade.NewProviderAdapterRegistry()
	installed := []providerplugin.Installed{
		{Manifest: providerplugin.Manifest{Name: "vision", Kind: Kind}},
		{Manifest: providerplugin.Manifest{Name: "camera", Kind: "board.provider"}},
	}
	if err := RegisterInstalled(registry, installed); err != nil {
		t.Fatal(err)
	}
	placeholder := func(context.Context, blockade.ProviderAdapterRuntime) (blockade.ProviderAdapter, error) {
		return nil, nil
	}
	if err := registry.Register("vision", placeholder); err == nil {
		t.Fatal("Blockade provider was not registered")
	}
	if err := registry.Register("camera", placeholder); err != nil {
		t.Fatalf("Board provider leaked into Blockade: %v", err)
	}
}
