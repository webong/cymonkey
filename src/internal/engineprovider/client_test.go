package engineprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cymonkey/src/internal/orchestrator"
)

func TestClientConnectsAndCallsEngine(t *testing.T) {
	registry := orchestrator.NewRegistry()
	adapter := &fakeEngineAdapter{}
	if err := registry.RegisterEngine("playwright", adapter); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := newTestServer(service.Routes())
	defer server.Close()

	client := NewClient(server.URL, "test-token")

	ctx := context.Background()

	// Health check.
	if err := client.Health(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}

	// List engines.
	engines, err := client.Engines(ctx)
	if err != nil {
		t.Fatalf("engines: %v", err)
	}
	if len(engines) == 0 {
		t.Fatal("no engines advertised")
	}

	// Connect.
	instance, err := client.Connect(ctx, ConnectRequest{
		APIVersion: APIVersion,
		InstanceID: "test-instance",
		Engine:     EngineSpec{Adapter: "playwright"},
		Target: Target{
			Kind:      "browser",
			Endpoints: []TargetEndpoint{{Name: "cdp", Protocol: "cdp", URL: "http://127.0.0.1:9222"}},
		},
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if instance.Status != "connected" {
		t.Fatalf("instance status = %q, want connected", instance.Status)
	}
	if len(instance.Capabilities) != 2 {
		t.Fatalf("capabilities = %#v", instance.Capabilities)
	}

	// Get instance.
	fetched, err := client.Get(ctx, "test-instance")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.InstanceID != "test-instance" || fetched.Status != "connected" {
		t.Fatalf("fetched = %#v", fetched)
	}

	// Call describe.
	callResult, err := client.Call(ctx, "test-instance", "describe", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if callResult.InstanceID != "test-instance" || callResult.Result == nil {
		t.Fatalf("call result = %#v", callResult)
	}

	// Delete.
	if err := client.Delete(ctx, "test-instance"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify deletion.
	if _, err := client.Get(ctx, "test-instance"); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestClientReconcile(t *testing.T) {
	registry := orchestrator.NewRegistry()
	adapter := &fakeEngineAdapter{}
	if err := registry.RegisterEngine("playwright", adapter); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	server := newTestServer(service.Routes())
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ctx := context.Background()

	result, err := client.Reconcile(ctx, ReconcileRequest{
		APIVersion: APIVersion,
		Desired: []ConnectRequest{
			{
				APIVersion: APIVersion,
				InstanceID: "one",
				Engine:     EngineSpec{Adapter: "playwright"},
				Target:     Target{Kind: "browser"},
			},
			{
				APIVersion: APIVersion,
				InstanceID: "two",
				Engine:     EngineSpec{Adapter: "playwright"},
				Target:     Target{Kind: "browser"},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Reconciled != 2 {
		t.Fatalf("reconciled = %d, want 2", result.Reconciled)
	}
	if len(result.Created) != 2 {
		t.Fatalf("created = %#v", result.Created)
	}
}

// newTestServer wraps a handler in an httptest.Server and returns it.
func newTestServer(handler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}) *testServer {
	ts := &testServer{}
	ts.Server = httptest.NewServer(handler)
	ts.URL = ts.Server.URL
	return ts
}

type testServer struct {
	*httptest.Server
	URL string
}
