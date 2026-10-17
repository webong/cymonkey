package serve

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"jangolova/displaykit"
)

type fakeSession struct {
	events   chan displaykit.Event
	updated  []int
	released bool
}

func (s *fakeSession) Capabilities() []string { return []string{"window.screenshot"} }
func (s *fakeSession) CallerLaunch() map[string]string {
	return map[string]string{"CONTROL_URL": "ws://127.0.0.1:1"}
}
func (s *fakeSession) Call(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"method":"` + method + `"}`), nil
}
func (s *fakeSession) Authorize(context.Context, displaykit.AuthorizeRequest) (displaykit.AuthorizeDecision, error) {
	return displaykit.AuthorizeDecision{Authorized: true}, nil
}
func (s *fakeSession) Health(context.Context) displaykit.Health {
	return displaykit.Health{Status: "connected", ObservedAt: time.Now()}
}
func (s *fakeSession) Events() <-chan displaykit.Event { return s.events }
func (s *fakeSession) UpdateMaterial(endpoint int, _ displaykit.Connection) error {
	s.updated = append(s.updated, endpoint)
	return nil
}
func (s *fakeSession) Disconnect(context.Context) error {
	s.released = true
	close(s.events)
	return nil
}

type fakeEngine struct {
	session *fakeSession
	target  displaykit.Target
	err     error
}

func (e *fakeEngine) Inspect(context.Context) displaykit.Inspection {
	return displaykit.Inspection{Available: true, Capabilities: []string{"window.screenshot"}}
}
func (e *fakeEngine) Connect(_ context.Context, _ displaykit.Spec, target displaykit.Target) (Session, error) {
	if e.err != nil {
		return nil, e.err
	}
	e.target = target
	e.session = &fakeSession{events: make(chan displaykit.Event, 8)}
	return e.session, nil
}

func call(t *testing.T, h *Handler, method string, params any, destination any) error {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		raw = encoded
	}
	result, err := h.Call(context.Background(), "display.engine."+method, raw)
	if err != nil {
		return err
	}
	if destination != nil {
		encoded, _ := json.Marshal(result)
		if err := json.Unmarshal(encoded, destination); err != nil {
			t.Fatal(err)
		}
	}
	return nil
}

func TestConnectOpensOneSessionAndReportsItsLaunchMaterial(t *testing.T) {
	engine := &fakeEngine{}
	h := NewHandler(engine)
	var connected displaykit.ConnectResult
	request := displaykit.ConnectRequest{Spec: displaykit.Spec{Adapter: "x"}, Target: displaykit.Target{TargetID: "t1", Kind: "macos-application",
		Endpoints: []displaykit.Endpoint{{Name: "cdp", Connection: &displaykit.Connection{Headers: map[string]string{"Authorization": "secret"}}}}}}
	if err := call(t, h, "connect", request, &connected); err != nil {
		t.Fatal(err)
	}
	if len(connected.Capabilities) != 1 || connected.CallerLaunch.Environment["CONTROL_URL"] == "" {
		t.Fatalf("connect result = %+v", connected)
	}
	if engine.target.TargetID != "t1" || engine.target.Endpoints[0].Connection.Headers["Authorization"] != "secret" {
		t.Fatalf("engine did not receive the target: %+v", engine.target)
	}
	if err := call(t, h, "connect", request, nil); err == nil {
		t.Fatal("a second session connected in one process")
	}
}

func TestOperationsRequireAConnectedSession(t *testing.T) {
	h := NewHandler(&fakeEngine{})
	for _, op := range []string{"call", "authorize", "health", "events", "material", "disconnect"} {
		params := any(map[string]any{"method": "x", "targetId": "t", "action": "a", "endpoint": 0, "connection": map[string]any{}})
		if err := call(t, h, op, params, nil); err == nil {
			t.Errorf("%s ran without a session", op)
		}
	}
}

func TestEventsAreReadByCursorAndOverflowDropsTheOldest(t *testing.T) {
	engine := &fakeEngine{}
	h := NewHandler(engine)
	if err := call(t, h, "connect", displaykit.ConnectRequest{}, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxBufferedEvents+10; i++ {
		engine.session.events <- displaykit.Event{Type: "e" + strconv.Itoa(i)}
	}
	deadline := time.Now().Add(2 * time.Second)
	var result displaykit.EventsResult
	for time.Now().Before(deadline) {
		if err := call(t, h, "events", displaykit.EventsRequest{}, &result); err != nil {
			t.Fatal(err)
		}
		if result.Cursor == strconv.Itoa(maxBufferedEvents+10) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(result.Events) != maxBufferedEvents || result.Events[0].Type != "e10" {
		t.Fatalf("buffered %d events, first %q", len(result.Events), result.Events[0].Type)
	}
	var again displaykit.EventsResult
	if err := call(t, h, "events", displaykit.EventsRequest{Cursor: result.Cursor}, &again); err != nil || len(again.Events) != 0 || again.Cursor != result.Cursor {
		t.Fatalf("a read at the cursor returned %+v, %v", again, err)
	}
	engine.session.events <- displaykit.Event{Type: "late"}
	for time.Now().Before(deadline) {
		if err := call(t, h, "events", displaykit.EventsRequest{Cursor: result.Cursor}, &again); err != nil {
			t.Fatal(err)
		}
		if len(again.Events) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(again.Events) != 1 || again.Events[0].Type != "late" {
		t.Fatalf("events after the cursor = %+v", again.Events)
	}
	if err := call(t, h, "events", displaykit.EventsRequest{Cursor: "not-a-number"}, nil); err == nil {
		t.Fatal("an invalid cursor was accepted")
	}
}

func TestMaterialUpdateAndDisconnectReachTheSession(t *testing.T) {
	engine := &fakeEngine{}
	h := NewHandler(engine)
	if err := call(t, h, "connect", displaykit.ConnectRequest{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := call(t, h, "material", displaykit.MaterialUpdate{Endpoint: 2}, nil); err != nil || len(engine.session.updated) != 1 || engine.session.updated[0] != 2 {
		t.Fatalf("material update = %v, %v", engine.session.updated, err)
	}
	if err := call(t, h, "disconnect", nil, nil); err != nil || !engine.session.released {
		t.Fatalf("disconnect = %v released=%v", err, engine.session.released)
	}
	if err := call(t, h, "call", displaykit.CallRequest{Method: "x"}, nil); err == nil {
		t.Fatal("a call ran after disconnect")
	}
}

func TestConnectFailureLeavesNoSession(t *testing.T) {
	engine := &fakeEngine{err: errors.New("no target")}
	h := NewHandler(engine)
	if err := call(t, h, "connect", displaykit.ConnectRequest{}, nil); err == nil {
		t.Fatal("a failed connect succeeded")
	}
	engine.err = nil
	if err := call(t, h, "connect", displaykit.ConnectRequest{}, nil); err != nil {
		t.Fatalf("connect after a failure: %v", err)
	}
}
