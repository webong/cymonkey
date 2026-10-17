package jangolovaplugin

import (
	"context"
	"testing"

	extplugin "github.com/webong/ext/pkg/plugin-go/contract"
)

func op(name string) extplugin.Request { return extplugin.Request{Operation: name} }

func TestProbeAdmitsOnlyInspect(t *testing.T) {
	authorize := probePolicy()
	ctx := context.Background()
	if err := authorize(ctx, op("inspect")); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"connect", "call", "events"} {
		if err := authorize(ctx, op(operation)); err == nil {
			t.Fatalf("a probe admitted %s", operation)
		}
	}
}

func TestSessionConnectsOnceBeforeAnythingElse(t *testing.T) {
	authorize := sessionPolicy()
	ctx := context.Background()
	for _, operation := range []string{"call", "events", "inspect"} {
		if err := authorize(ctx, op(operation)); err == nil {
			t.Fatalf("a session admitted %s before connect", operation)
		}
	}
	if err := authorize(ctx, op("connect")); err != nil {
		t.Fatal(err)
	}
	if err := authorize(ctx, op("connect")); err == nil {
		t.Fatal("a session admitted connect twice")
	}
	for _, operation := range []string{"call", "authorize", "health", "events", "material", "disconnect"} {
		if err := authorize(ctx, op(operation)); err != nil {
			t.Fatalf("a connected session refused %s: %v", operation, err)
		}
	}
	if err := authorize(ctx, op("inspect")); err == nil {
		t.Fatal("a session admitted inspect")
	}
}
