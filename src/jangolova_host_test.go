package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	"jangolova/sdk"
)

func TestTargetBoundaryPreservesHostRotationAndIsolatesMetadata(t *testing.T) {
	material := &orchestrator.EndpointConnection{}
	revision := material.ReplaceCredential(map[string]string{"Authorization": "Bearer fixture"}, time.Now().Add(time.Hour))
	original := orchestrator.EngineTarget{Kind: "blender", Metadata: map[string]string{"runtime": "blender"}, Endpoints: []orchestrator.TargetEndpoint{{Protocol: "websocket", URL: "ws://127.0.0.1:9321", Metadata: map[string]string{"scope": "fixture"}, Connection: material}}}
	target := jangolovaTarget(original)
	target.Metadata["runtime"] = "changed"
	target.Endpoints[0].Metadata["scope"] = "changed"
	if original.Metadata["runtime"] != "blender" || original.Endpoints[0].Metadata["scope"] != "fixture" {
		t.Fatal("module mutated host metadata")
	}
	view := target.Endpoints[0].Connection
	snapshot := view.Snapshot()
	snapshot.Headers["Authorization"] = "changed"
	if material.Snapshot().Headers["Authorization"] != "Bearer fixture" {
		t.Fatal("module mutated stored credentials")
	}
	updates := view.Updates()
	next := material.ReplaceCredential(map[string]string{"Authorization": "Bearer rotated"}, time.Now().Add(time.Hour))
	select {
	case got := <-updates:
		if got != next {
			t.Fatal("revision lost")
		}
	case <-time.After(time.Second):
		t.Fatal("no rotation notification")
	}
	if next <= revision || view.Snapshot().Headers["Authorization"] != "Bearer rotated" {
		t.Fatal("rotation not visible")
	}
	view.Acknowledge(next)
	_, ack := material.Acknowledgements()
	if ack != next {
		t.Fatal("acknowledgement did not reach host")
	}
	if err := jangolovaServices().Validate(target.Endpoints[0]); err != nil {
		t.Fatal(err)
	}
	material.ReplaceCredential(map[string]string{"Authorization": "Bearer expired"}, time.Now().Add(-time.Minute))
	if err := jangolovaServices().Validate(target.Endpoints[0]); err == nil {
		t.Fatal("expired host material accepted")
	}
}

type fixtureAdapter struct{ session *fixtureSession }

func (a fixtureAdapter) Connect(context.Context, sdk.EngineSpec, sdk.EngineTarget) (sdk.EngineInstance, error) {
	return a.session, nil
}

type fixtureSession struct {
	events chan sdk.EngineEvent
	closed bool
}

func (s *fixtureSession) Disconnect(context.Context) error {
	s.closed = true
	close(s.events)
	return nil
}
func (s *fixtureSession) Authorize(context.Context, sdk.AuthorizeRequest) (sdk.AuthorizeDecision, error) {
	return sdk.AuthorizeDecision{Authorized: true}, nil
}
func (s *fixtureSession) Call(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"ok":true}`), nil
}
func (s *fixtureSession) EngineEvents() <-chan sdk.EngineEvent { return s.events }
func (s *fixtureSession) EngineCallerLaunch() sdk.CallerLaunch {
	return sdk.CallerLaunch{Environment: map[string]string{
		"JANGOLOVA_CONTROL_URL":      "ws://127.0.0.1:7391",
		"JANGOLOVA_CONTROL_TOKEN":    "fixture",
		"JANGOLOVA_CONTROL_PROTOCOL": "fixture/v1",
	}}
}
func TestHostBindingForwardsCallsEventsAndDisconnect(t *testing.T) {
	source := &fixtureSession{events: make(chan sdk.EngineEvent, 1)}
	instance, err := wrapJangolova(fixtureAdapter{source}).Connect(context.Background(), manifest.EngineSpec{}, orchestrator.EngineTarget{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := instance.(sdk.Caller).Call(context.Background(), "health", nil)
	if err != nil || string(raw) != `{"ok":true}` {
		t.Fatalf("call: %s %v", raw, err)
	}
	launch := instance.(orchestrator.EngineCallerLaunchProvider).EngineCallerLaunch()
	if launch.Environment["JANGOLOVA_CYMONKEY_CONTROL_URL"] != "ws://127.0.0.1:7391" ||
		launch.Environment["JANGOLOVA_CYMONKEY_CONTROL_TOKEN"] != "fixture" ||
		launch.Environment["JANGOLOVA_CYMONKEY_PROTOCOL"] != "fixture/v1" ||
		launch.Environment["JANGOLOVA_CONTROL_URL"] != "" {
		t.Fatalf("host launch compatibility = %#v", launch.Environment)
	}
	events := instance.(orchestrator.EngineEventSource).EngineEvents()
	source.events <- sdk.EngineEvent{Type: "jangolova.connected", Status: "connected"}
	select {
	case e := <-events:
		if e.Type != "cymonkey.connected" {
			t.Fatalf("host event compatibility = %q", e.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("event not forwarded")
	}
	if err := instance.Disconnect(context.Background()); err != nil || !source.closed {
		t.Fatal("module not disconnected")
	}
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("event stream still open")
		}
	case <-time.After(time.Second):
		t.Fatal("event forwarding leaked after disconnect")
	}
}
