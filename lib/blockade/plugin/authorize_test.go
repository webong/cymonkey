package blockadeplugin

import (
	"context"
	"testing"

	extplugin "github.com/webong/ext/pkg/plugin-go/contract"
)

func op(name string) extplugin.Request { return extplugin.Request{Operation: name} }

func TestWorkerStartsOnceBeforeAnythingElse(t *testing.T) {
	authorize := workerPolicy()
	ctx := context.Background()
	for _, operation := range []string{"observe", "interpret", "capabilities", "health", "close", "model.inspect"} {
		if err := authorize(ctx, op(operation)); err == nil {
			t.Fatalf("a worker admitted %s before start", operation)
		}
	}
	if err := authorize(ctx, op("start")); err != nil {
		t.Fatal(err)
	}
	if err := authorize(ctx, op("start")); err == nil {
		t.Fatal("a worker admitted start twice")
	}
	for _, operation := range []string{"capabilities", "health", "observe", "interpret", "close"} {
		if err := authorize(ctx, op(operation)); err != nil {
			t.Fatalf("a started worker refused %s: %v", operation, err)
		}
	}
	if err := authorize(ctx, op("model.inspect")); err == nil {
		t.Fatal("a worker admitted model.inspect")
	}
}

func TestInspectProcessAdmitsOnlyModelInspect(t *testing.T) {
	authorize := inspectPolicy()
	ctx := context.Background()
	if err := authorize(ctx, op("model.inspect")); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"start", "observe", "interpret", "close"} {
		if err := authorize(ctx, op(operation)); err == nil {
			t.Fatalf("an inspect process admitted %s", operation)
		}
	}
}
