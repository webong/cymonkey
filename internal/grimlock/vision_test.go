package grimlock

import (
	"context"
	"testing"

	"jangolova/internal/blockade"
)

type visionProviderFixture struct{ protocol string }

func (p visionProviderFixture) Protocol() string { return p.protocol }
func (p visionProviderFixture) Observe(context.Context, blockade.ObserveRequest) (blockade.ObserveResponse, error) {
	return blockade.ObserveResponse{APIVersion: blockade.APIVersion}, nil
}

func TestVisionProviderRegistry(t *testing.T) {
	registry, err := NewVisionProviderRegistry(visionProviderFixture{protocol: "fixture-vision"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Provider("fixture-vision"); !ok {
		t.Fatal("vision provider not registered")
	}
	if got := registry.Protocols(); len(got) != 1 || got[0] != "fixture-vision" {
		t.Fatalf("protocols = %#v", got)
	}
	if err := registry.Register(visionProviderFixture{protocol: "fixture-vision"}); err == nil {
		t.Fatal("duplicate provider accepted")
	}
}
