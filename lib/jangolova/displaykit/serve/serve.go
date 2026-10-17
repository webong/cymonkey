// Package serve runs a display engine as an ext package that serves the
// display.engine contract. A package main supplies an Engine and this package
// speaks the protocol: one session per process, with the session's events
// buffered so the host can read them by cursor.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"github.com/webong/ext/pkg/plugin-go/contract/transport/jsonline"
	"jangolova/displaykit"
)

// maxBufferedEvents bounds the events kept for a host that reads slowly; the
// oldest are dropped first.
const maxBufferedEvents = 256

// Engine is what a display engine program implements.
type Engine interface {
	Inspect(ctx context.Context) displaykit.Inspection
	Connect(ctx context.Context, spec displaykit.Spec, target displaykit.Target) (Session, error)
}

// Session is one connection of the engine to a target.
type Session interface {
	Capabilities() []string
	CallerLaunch() map[string]string
	Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
	Authorize(ctx context.Context, request displaykit.AuthorizeRequest) (displaykit.AuthorizeDecision, error)
	Health(ctx context.Context) displaykit.Health
	// Events delivers lifecycle events; it is closed when the session ends.
	Events() <-chan displaykit.Event
	// UpdateMaterial replaces the prepared material of one endpoint.
	UpdateMaterial(endpoint int, connection displaykit.Connection) error
	Disconnect(ctx context.Context) error
}

// Run loads the descriptor beside the executable and serves until the host
// closes the session.
func Run(ctx context.Context, engine Engine) error {
	path := filepath.Join(filepath.Dir(os.Args[0]), store.ManifestName)
	descriptor, err := packagekit.LoadDescriptor(path)
	if err != nil {
		return err
	}
	if !(packagekit.Manifest{Descriptor: descriptor}).Supports(displaykit.ContractName, displaykit.ContractVersion) {
		return errors.New("plugin manifest does not declare the display.engine contract")
	}
	return jsonline.ServeFunc(ctx, descriptor, NewHandler(engine).Call, jsonline.FuncOptions{})
}

// Handler serves the display.engine operations for one Engine.
type Handler struct {
	engine Engine

	mu      sync.Mutex
	session Session
	drained chan struct{}

	events eventLog
}

func NewHandler(engine Engine) *Handler { return &Handler{engine: engine} }

type eventLog struct {
	mu     sync.Mutex
	events []displaykit.Event
	first  uint64 // events dropped so far
}

func (l *eventLog) add(event displaykit.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.events) == maxBufferedEvents {
		l.events = l.events[1:]
		l.first++
	}
	l.events = append(l.events, event)
}

// after returns the events with a sequence number above cursor and the new
// cursor. Sequence numbers start at 1; first counts the events already dropped,
// so events[i] has sequence number first+i+1.
func (l *eventLog) after(cursor uint64) ([]displaykit.Event, uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	last := l.first + uint64(len(l.events))
	if cursor >= last {
		return []displaykit.Event{}, last
	}
	skip := 0
	if cursor > l.first {
		skip = int(cursor - l.first)
	}
	return append([]displaykit.Event(nil), l.events[skip:]...), last
}

// Call dispatches one display.engine operation.
func (h *Handler) Call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case displaykit.ContractName + ".inspect":
		return h.engine.Inspect(ctx), nil
	case displaykit.ContractName + ".connect":
		var request displaykit.ConnectRequest
		if err := decode(raw, &request); err != nil {
			return nil, err
		}
		return h.connect(ctx, request)
	}
	h.mu.Lock()
	session := h.session
	h.mu.Unlock()
	if session == nil {
		return nil, errors.New("no display session is connected")
	}
	switch method {
	case displaykit.ContractName + ".call":
		var request displaykit.CallRequest
		if err := decode(raw, &request); err != nil {
			return nil, err
		}
		return session.Call(ctx, request.Method, request.Params)
	case displaykit.ContractName + ".authorize":
		var request displaykit.AuthorizeRequest
		if err := decode(raw, &request); err != nil {
			return nil, err
		}
		return session.Authorize(ctx, request)
	case displaykit.ContractName + ".health":
		return session.Health(ctx), nil
	case displaykit.ContractName + ".events":
		var request displaykit.EventsRequest
		if len(raw) != 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &request); err != nil {
				return nil, errors.New("decode events request")
			}
		}
		var cursor uint64
		if request.Cursor != "" {
			parsed, err := strconv.ParseUint(request.Cursor, 10, 64)
			if err != nil {
				return nil, errors.New("events cursor is not valid")
			}
			cursor = parsed
		}
		events, next := h.events.after(cursor)
		return displaykit.EventsResult{Cursor: strconv.FormatUint(next, 10), Events: events}, nil
	case displaykit.ContractName + ".material":
		var request displaykit.MaterialUpdate
		if err := decode(raw, &request); err != nil {
			return nil, err
		}
		return struct{}{}, session.UpdateMaterial(request.Endpoint, request.Connection)
	case displaykit.ContractName + ".disconnect":
		return struct{}{}, h.disconnect(ctx)
	}
	return nil, fmt.Errorf("unsupported method %q", method)
}

func decode(raw json.RawMessage, destination any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return errors.New("method parameters are required")
	}
	return json.Unmarshal(raw, destination)
}

func (h *Handler) connect(ctx context.Context, request displaykit.ConnectRequest) (any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.session != nil {
		return nil, errors.New("a display session is already connected")
	}
	session, err := h.engine.Connect(ctx, request.Spec, request.Target)
	if err != nil {
		return nil, err
	}
	h.session = session
	h.drained = make(chan struct{})
	go func(events <-chan displaykit.Event, done chan struct{}) {
		defer close(done)
		for event := range events {
			h.events.add(event)
		}
	}(session.Events(), h.drained)
	return displaykit.ConnectResult{
		Capabilities: session.Capabilities(),
		CallerLaunch: displaykit.CallerLaunch{Environment: session.CallerLaunch()},
	}, nil
}

func (h *Handler) disconnect(ctx context.Context) error {
	h.mu.Lock()
	session, drained := h.session, h.drained
	h.session, h.drained = nil, nil
	h.mu.Unlock()
	if session == nil {
		return nil
	}
	err := session.Disconnect(ctx)
	// The session should close its event stream when it ends; do not hang if
	// it does not.
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
	}
	return err
}
