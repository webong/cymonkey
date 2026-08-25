package cymonkey_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"jangolova/internal/bridge"
	"jangolova/internal/cymonkey"
	"jangolova/internal/pacman"
)

type mockPacmanCaller struct {
	helloResult        json.RawMessage
	capabilitiesResult json.RawMessage
	describeResult     json.RawMessage
	actResult          json.RawMessage
	eventsResult       json.RawMessage
	healthResult       json.RawMessage
	lastActParams      json.RawMessage
}

func (m *mockPacmanCaller) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case bridge.MethodHello:
		if m.helloResult != nil {
			return m.helloResult, nil
		}
		return json.Marshal(pacman.Hello{
			ProtocolVersion: pacman.ProtocolVersion,
			Implementation: pacman.Implementation{
				Engine:  "godot",
				Name:    "godot-pacman",
				Version: "1.0.0",
			},
			Features: []string{"explicit-registration"},
		})
	case bridge.MethodCapabilities:
		if m.capabilitiesResult != nil {
			return m.capabilitiesResult, nil
		}
		return json.Marshal([]pacman.Capability{
			{
				Name:        "pacman.scene.update",
				Description: "Update scene",
				Effect:      "write",
				TargetKinds: []pacman.ResourceKind{pacman.KindScene},
				InputSchema: json.RawMessage(`{}`),
			},
		})
	case bridge.MethodDescribe:
		if m.describeResult != nil {
			return m.describeResult, nil
		}
		return json.Marshal(pacman.Description{
			Revision: "rev-100",
			Resources: []pacman.Resource{
				{
					ID:         "scene:main",
					Kind:       pacman.KindScene,
					Label:      "Main Scene",
					Properties: json.RawMessage(`{"loaded":true}`),
				},
			},
		})
	case bridge.MethodAct:
		m.lastActParams = params
		if m.actResult != nil {
			return m.actResult, nil
		}
		return json.RawMessage(`{"status":"ok"}`), nil
	case bridge.MethodEvents:
		if m.eventsResult != nil {
			return m.eventsResult, nil
		}
		return json.Marshal(pacman.EventBatch{
			Events: []pacman.Event{},
			Cursor: "0",
		})
	case "health":
		if m.healthResult != nil {
			return m.healthResult, nil
		}
		return json.RawMessage(`{"status":"ready"}`), nil
	default:
		return nil, errors.New("unknown method")
	}
}

func TestPacmanAdapter(t *testing.T) {
	mock := &mockPacmanCaller{}
	adapter := cymonkey.NewPacmanAdapter(mock)
	ctx := context.Background()

	t.Run("hello translation", func(t *testing.T) {
		res, err := adapter.Call(ctx, bridge.MethodHello, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("hello failed: %v", err)
		}
		var hello cymonkey.Hello
		if err := json.Unmarshal(res, &hello); err != nil {
			t.Fatalf("unmarshal hello: %v", err)
		}
		if hello.ProtocolVersion != cymonkey.ProtocolVersion {
			t.Errorf("expected protocolVersion %s, got %s", cymonkey.ProtocolVersion, hello.ProtocolVersion)
		}
		if len(hello.Profiles) != 1 || hello.Profiles[0] != cymonkey.ProfileEngine {
			t.Errorf("expected profile engine, got %v", hello.Profiles)
		}
		if len(hello.Backends) != 1 || hello.Backends[0] != cymonkey.BackendEngineGodot {
			t.Errorf("expected backend engine-godot, got %v", hello.Backends)
		}
	})

	t.Run("capabilities translation", func(t *testing.T) {
		res, err := adapter.Call(ctx, bridge.MethodCapabilities, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("capabilities failed: %v", err)
		}
		var caps []cymonkey.Capability
		if err := json.Unmarshal(res, &caps); err != nil {
			t.Fatalf("unmarshal capabilities: %v", err)
		}
		if len(caps) != 1 {
			t.Fatalf("expected 1 capability, got %d", len(caps))
		}
		if caps[0].Name != "pacman.scene.update" || caps[0].Profile != cymonkey.ProfileEngine {
			t.Errorf("unexpected capability: %+v", caps[0])
		}
		if len(caps[0].TargetKinds) != 1 || caps[0].TargetKinds[0] != "scene" {
			t.Errorf("expected targetKind scene, got %v", caps[0].TargetKinds)
		}
	})

	t.Run("describe translation and revision caching", func(t *testing.T) {
		res, err := adapter.Call(ctx, bridge.MethodDescribe, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("describe failed: %v", err)
		}
		var desc cymonkey.Description
		if err := json.Unmarshal(res, &desc); err != nil {
			t.Fatalf("unmarshal describe: %v", err)
		}
		if desc.Revision != "rev-100" {
			t.Errorf("expected revision rev-100, got %s", desc.Revision)
		}
		if len(desc.Surfaces) != 1 || desc.Surfaces[0].ID != "scene:main" {
			t.Errorf("unexpected surface: %+v", desc.Surfaces)
		}
	})

	t.Run("act with valid revision", func(t *testing.T) {
		actPayload := `{"name":"pacman.scene.update","input":{"targetId":"scene:main","expectedRevision":"rev-100"}}`
		res, err := adapter.Call(ctx, bridge.MethodAct, json.RawMessage(actPayload))
		if err != nil {
			t.Fatalf("act failed: %v", err)
		}
		if string(res) != `{"status":"ok"}` {
			t.Errorf("unexpected act result: %s", res)
		}

		var pacmanReq pacman.ActionRequest
		if err := json.Unmarshal(mock.lastActParams, &pacmanReq); err != nil {
			t.Fatalf("unmarshal translated pacman action request: %v", err)
		}
		if pacmanReq.Name != "pacman.scene.update" || pacmanReq.TargetID != "scene:main" {
			t.Errorf("unexpected pacman action request: %+v", pacmanReq)
		}
	})

	t.Run("act with stale revision fails", func(t *testing.T) {
		actPayload := `{"name":"pacman.scene.update","input":{"targetId":"scene:main","expectedRevision":"rev-999"}}`
		_, err := adapter.Call(ctx, bridge.MethodAct, json.RawMessage(actPayload))
		if err == nil {
			t.Fatal("expected error for stale revision, got nil")
		}
		if err.Error() != "stale revision: expected rev-999, got rev-100" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
