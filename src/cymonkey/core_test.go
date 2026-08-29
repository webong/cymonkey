package cymonkey

import (
	"context"
	"encoding/json"
	"testing"
)

type testAttachment struct{ Caller }

func (testAttachment) Close(context.Context) error { return nil }

type testCaller struct {
	domain  Domain
	runtime string
	acted   bool
}

func (caller *testCaller) Call(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "hello":
		return json.Marshal(Hello{
			ProtocolVersion: ProtocolVersion,
			Domains:         []Domain{caller.domain},
			Runtimes:        []string{caller.runtime},
			Drivers:         []string{"test-driver"},
		})
	case "capabilities":
		return json.Marshal([]Capability{{
			Name:        "overlay.mount",
			Domain:      caller.domain,
			Runtime:     caller.runtime,
			Driver:      "test-driver",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}})
	case "describe", "events":
		return json.RawMessage(`{}`), nil
	case "act":
		caller.acted = true
		return json.RawMessage(`{"ok":true}`), nil
	default:
		return nil, nil
	}
}

func TestStandaloneRegistryAndComposite(t *testing.T) {
	caller := &testCaller{domain: DomainRender, runtime: "example-runtime"}
	module := ModuleFunc{
		ModuleDescriptor: ModuleDescriptor{
			ID:              "render.example",
			Kind:            RuntimeModule,
			ProtocolVersion: ProtocolVersion,
			Runtimes:        []RuntimeBinding{{Domain: DomainRender, Runtime: "example-runtime"}},
			Drivers:         []DriverDescriptor{{ID: "test-driver", Transports: []string{"example-rpc"}}},
		},
		AttachTarget: func(context.Context, AttachOptions) (Attachment, error) {
			return testAttachment{Caller: caller}, nil
		},
	}
	registry, err := NewRegistry(module)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.Resolve("", DomainRender, "example-runtime", "test-driver")
	if err != nil || resolved.Descriptor().ID != "render.example" {
		t.Fatalf("resolve standalone module: %v", err)
	}
	attachment, err := resolved.Attach(context.Background(), AttachOptions{Target: Target{Kind: "example-runtime"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateConformance(context.Background(), attachment); err != nil {
		t.Fatalf("portable conformance: %v", err)
	}
	composite, err := NewComposite(Binding{ID: "scene", Caller: attachment})
	if err != nil {
		t.Fatal(err)
	}
	_, err = composite.Call(context.Background(), "act", json.RawMessage(`{"name":"overlay.mount","domain":"render","runtime":"example-runtime","input":{}}`))
	if err != nil {
		t.Fatalf("route composite action: %v", err)
	}
	if !caller.acted {
		t.Fatal("composite did not call the standalone runtime")
	}
}
