package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"cymonkey/src/jangolova/sdk"
)

type activatedFixture struct {
	closed       bool
	wrongRuntime bool
	unhealthy    bool
	calls        []string
}

func (f *activatedFixture) Disconnect(context.Context) error { f.closed = true; return nil }
func (f *activatedFixture) Authorize(context.Context, sdk.AuthorizeRequest) (sdk.AuthorizeDecision, error) {
	return sdk.AuthorizeDecision{}, nil
}
func (f *activatedFixture) Call(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	switch method {
	case "hello":
		runtime := "blender"
		if f.wrongRuntime {
			runtime = "unity"
		}
		return json.Marshal(map[string]any{"protocolVersion": "cymonkey/v1alpha1", "implementation": map[string]string{"name": "fixture"}, "domains": []string{"render"}, "runtimes": []string{runtime}, "drivers": []string{"websocket"}})
	case "capabilities":
		return json.RawMessage(`[{"name":"render.frame","domain":"render","runtime":"blender","driver":"websocket","support":"native","lifetime":"attachment","persistence":"session","effect":"write","inputSchema":{"type":"object"}}]`), nil
	case "describe":
		return json.RawMessage(`{"revision":"1","surfaces":[],"augmentations":[]}`), nil
	case "health":
		if f.unhealthy {
			return json.RawMessage(`{"status":"unhealthy"}`), nil
		}
		return json.RawMessage(`{"status":"ready"}`), nil
	default:
		return nil, errors.New("unsupported")
	}
}

func TestActivationClosesUnhealthySession(t *testing.T) {
	m, p := activationFixture(t)
	session := &activatedFixture{unhealthy: true}
	_, err := Activate(context.Background(), m, "test", p, Activation{
		Approve: func(context.Context, Module, Artifact) error { return nil },
		Mount:   func(context.Context, Module, io.Reader) (sdk.EngineInstance, error) { return session, nil },
	})
	if err == nil || !session.closed {
		t.Fatal("unhealthy activated session was not closed")
	}
}
func activationFixture(t *testing.T) (Module, string) {
	t.Helper()
	content := []byte("reviewed plugin bytes")
	sum := sha256.Sum256(content)
	m := Module{ID: "render/blender", Version: "0.1.0", Runtime: "blender", ProtocolVersion: "cymonkey/v1alpha1", Status: "available", Platforms: []string{"test"}, Actions: []string{"render.frame"}, Artifacts: []Artifact{{Platform: "test", URL: "https://example.test/module.py", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))}}}
	p := filepath.Join(t.TempDir(), "plugin.py")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return m, p
}
func TestActivationRequiresApprovalAndReverifiesCache(t *testing.T) {
	for _, scenario := range []string{"missing approval", "denied", "tampered", "revoked", "oversize"} {
		t.Run(scenario, func(t *testing.T) {
			m, p := activationFixture(t)
			mounts := 0
			h := Activation{Approve: func(context.Context, Module, Artifact) error { return nil }, Mount: func(context.Context, Module, io.Reader) (sdk.EngineInstance, error) {
				mounts++
				return &activatedFixture{}, nil
			}}
			switch scenario {
			case "missing approval":
				h.Approve = nil
			case "denied":
				h.Approve = func(context.Context, Module, Artifact) error { return errors.New("denied") }
			case "tampered":
				_ = os.WriteFile(p, []byte("modified"), 0o600)
			case "revoked":
				m.Status = "revoked"
			case "oversize":
				h.MaxBytes = 2
			}
			if _, err := Activate(context.Background(), m, "test", p, h); err == nil {
				t.Fatal("activation unexpectedly succeeded")
			}
			if mounts != 0 {
				t.Fatal("denied artifact reached mount callback")
			}
		})
	}
}
func TestActivationPassesVerifiedBytesAndChecksRuntime(t *testing.T) {
	for _, wrongRuntime := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "wrong runtime closes session"}[wrongRuntime], func(t *testing.T) {
			m, p := activationFixture(t)
			session := &activatedFixture{wrongRuntime: wrongRuntime}
			h := Activation{Approve: func(_ context.Context, copy Module, _ Artifact) error {
				copy.Actions[0] = "bad.action"
				return os.WriteFile(p, []byte("replaced after approval"), 0o600)
			}, Mount: func(_ context.Context, copy Module, verified io.Reader) (sdk.EngineInstance, error) {
				b, err := io.ReadAll(verified)
				if err != nil {
					return nil, err
				}
				if string(b) != "reviewed plugin bytes" || copy.Actions[0] != "render.frame" {
					t.Fatal("mutable artifact or metadata crossed approval boundary")
				}
				return session, nil
			}}
			result, err := Activate(context.Background(), m, "test", p, h)
			if wrongRuntime {
				if err == nil || !session.closed {
					t.Fatal("incompatible module was not disconnected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result != session {
				t.Fatal("wrong session")
			}
			if len(session.calls) != 4 {
				t.Fatalf("negotiation calls: %v", session.calls)
			}
		})
	}
}
