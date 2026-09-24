// Package displayinteraction implements Jangolova's provider-neutral
// display-level interaction engine for targets without semantic APIs.
package displayinteraction

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"cymonkey/src/internal/bridge"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
)

// Supported protocols for display interaction targets.
const (
	ProtocolVNC        = "vnc"
	ProtocolRFB        = "rfb"
	ProtocolWebRTC     = "webrtc"
	ProtocolWaylandRFB = "wayland-rfb"
)

// Standard display interaction action names.
const (
	ActionDisplayDescribe = "display.describe"
	ActionDisplayCapture  = "display.capture"
	ActionPointerMove     = "pointer.move"
	ActionPointerClick    = "pointer.click"
	ActionPointerDrag     = "pointer.drag"
	ActionPointerScroll   = "pointer.scroll"
	ActionKeyboardType    = "keyboard.type"
	ActionKeyboardPress   = "keyboard.press"
)

type DisplayBounds struct {
	MinX int `json:"minX"`
	MinY int `json:"minY"`
	MaxX int `json:"maxX"`
	MaxY int `json:"maxY"`
}

type Policy struct {
	AllowedBounds        *DisplayBounds `json:"allowedBounds,omitempty"`
	MaxTextLength        int            `json:"maxTextLength,omitempty"`
	BlockedKeys          []string       `json:"blockedKeys,omitempty"`
	RedactSensitiveInput bool           `json:"redactSensitiveInput,omitempty"`
}

func (p Policy) ValidateCoordinates(x, y int) error {
	if p.AllowedBounds == nil {
		return nil
	}
	b := p.AllowedBounds
	if b.MaxX > 0 || b.MaxY > 0 {
		if x < b.MinX || x > b.MaxX || y < b.MinY || y > b.MaxY {
			return fmt.Errorf("coordinates (%d, %d) exceed allowed bounds [%d,%d - %d,%d]", x, y, b.MinX, b.MinY, b.MaxX, b.MaxY)
		}
	}
	return nil
}

func (p Policy) ValidateText(text string) error {
	if p.MaxTextLength > 0 && len(text) > p.MaxTextLength {
		return fmt.Errorf("text length %d exceeds max allowed length %d", len(text), p.MaxTextLength)
	}
	return nil
}

func (p Policy) ValidateKey(key string) error {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, blocked := range p.BlockedKeys {
		if strings.ToLower(strings.TrimSpace(blocked)) == k {
			return fmt.Errorf("key %q is blocked by display policy", key)
		}
	}
	return nil
}

// authorize is the per-capability policy gate for an action invocation. It
// runs the same coordinate/text/key checks that dispatchAction enforces, so
// callers can decide whether to permit an action before it is dispatched.
func (p Policy) authorize(name string, input json.RawMessage) error {
	switch name {
	case ActionPointerMove, ActionPointerClick, ActionPointerScroll:
		var v struct{ X, Y int }
		_ = json.Unmarshal(input, &v)
		return p.ValidateCoordinates(v.X, v.Y)
	case ActionPointerDrag:
		var v struct{ StartX, StartY, EndX, EndY int }
		_ = json.Unmarshal(input, &v)
		if err := p.ValidateCoordinates(v.StartX, v.StartY); err != nil {
			return err
		}
		return p.ValidateCoordinates(v.EndX, v.EndY)
	case ActionKeyboardType:
		var v struct{ Text string }
		_ = json.Unmarshal(input, &v)
		return p.ValidateText(v.Text)
	case ActionKeyboardPress:
		var v struct{ Key string }
		_ = json.Unmarshal(input, &v)
		return p.ValidateKey(v.Key)
	}
	return nil
}

type Adapter struct {
	Connector Connector
	Policy    Policy
}

type Connector interface {
	Connect(ctx context.Context, endpoint orchestrator.TargetEndpoint) (Transport, error)
}

type Transport interface {
	Protocol() string
	Describe(ctx context.Context) (DisplayDescription, error)
	Capture(ctx context.Context) (FrameCapture, error)
	PointerMove(ctx context.Context, x, y int) error
	PointerClick(ctx context.Context, x, y int, button string, count int) error
	PointerDrag(ctx context.Context, startX, startY, endX, endY int) error
	PointerScroll(ctx context.Context, x, y, deltaX, deltaY int) error
	KeyboardType(ctx context.Context, text string) error
	KeyboardPress(ctx context.Context, key string) error
	Close() error
}

type DisplayDescription struct {
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ColorDepth  int    `json:"colorDepth"`
	Title       string `json:"title,omitempty"`
	Focused     bool   `json:"focused"`
	Orientation string `json:"orientation,omitempty"`
}

type FrameCapture struct {
	Format     string `json:"format"` // e.g. "image/png"
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Base64Data string `json:"base64Data"`
}

type instance struct {
	callMu       sync.Mutex
	stateMu      sync.RWMutex
	transport    Transport
	endpoint     orchestrator.TargetEndpoint
	capabilities []string
	policy       Policy
	disconnected bool
	events       chan orchestrator.EngineEvent
	renewalStop  chan struct{}
	renewalOnce  sync.Once
	renewalWG    sync.WaitGroup
}

var (
	_ orchestrator.EngineAdapter            = Adapter{}
	_ orchestrator.EngineInspector          = Adapter{}
	_ orchestrator.EngineInstance           = (*instance)(nil)
	_ orchestrator.EngineHealthProvider     = (*instance)(nil)
	_ orchestrator.EngineCapabilityProvider = (*instance)(nil)
	_ orchestrator.EngineEventSource        = (*instance)(nil)
	_ bridge.Caller                         = (*instance)(nil)
)

func (a Adapter) connector() Connector {
	if a.Connector != nil {
		return a.Connector
	}
	return defaultConnector{}
}

func (a Adapter) InspectEngine(context.Context) orchestrator.EngineInspection {
	capabilities := []string{
		"act", "capabilities", "describe", "events", "health",
		"target.vnc", "target.rfb", "target.webrtc", "target.wayland-rfb",
		ActionDisplayDescribe, ActionDisplayCapture,
		ActionPointerMove, ActionPointerClick, ActionPointerDrag, ActionPointerScroll,
		ActionKeyboardType, ActionKeyboardPress,
	}
	sort.Strings(capabilities)
	return orchestrator.EngineInspection{
		Available:    true,
		Capabilities: capabilities,
	}
}

func (a Adapter) Connect(ctx context.Context, spec manifest.EngineSpec, target orchestrator.EngineTarget) (orchestrator.EngineInstance, error) {
	validKinds := map[string]bool{
		"display":           true,
		"linux-application": true,
		"native":            true,
		"vm":                true,
		"container":         true,
		"browser":           true,
	}
	if !validKinds[target.Kind] {
		return nil, fmt.Errorf("display-interaction target.kind %q is not supported", target.Kind)
	}

	endpoint, ok := findDisplayEndpoint(target)
	if !ok {
		return nil, errors.New("display-interaction requires a target endpoint with protocol vnc, rfb, webrtc, or wayland-rfb")
	}

	connector := a.connector()
	transport, err := connector.Connect(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("connect display-interaction endpoint %s: %w", endpoint.URL, err)
	}

	policy := a.Policy
	if len(spec.Options) > 0 {
		var opts struct {
			Policy Policy `json:"policy"`
		}
		if err := json.Unmarshal(spec.Options, &opts); err == nil && (opts.Policy.AllowedBounds != nil || opts.Policy.MaxTextLength > 0 || len(opts.Policy.BlockedKeys) > 0) {
			policy = opts.Policy
		} else {
			_ = json.Unmarshal(spec.Options, &policy)
		}
	}
	if target.Metadata != nil {
		if policyStr, exists := target.Metadata["display.policy"]; exists && policyStr != "" {
			_ = json.Unmarshal([]byte(policyStr), &policy)
		}
	}

	capabilities := []string{
		"act", "capabilities", "describe", "events", "health",
		"target." + strings.ToLower(endpoint.Protocol),
		ActionDisplayDescribe, ActionDisplayCapture,
		ActionPointerMove, ActionPointerClick, ActionPointerDrag, ActionPointerScroll,
		ActionKeyboardType, ActionKeyboardPress,
	}
	sort.Strings(capabilities)

	inst := &instance{
		transport:    transport,
		endpoint:     endpoint,
		capabilities: capabilities,
		policy:       policy,
		events:       make(chan orchestrator.EngineEvent, 16),
		renewalStop:  make(chan struct{}),
	}

	inst.emit(orchestrator.EngineEvent{
		Type:       "display.connected",
		Status:     orchestrator.EngineHealthHealthy,
		OccurredAt: time.Now().UTC(),
	})

	return inst, nil
}

func findDisplayEndpoint(target orchestrator.EngineTarget) (orchestrator.TargetEndpoint, bool) {
	protocols := []string{ProtocolVNC, ProtocolRFB, ProtocolWebRTC, ProtocolWaylandRFB}
	for _, proto := range protocols {
		if ep, ok := target.Endpoint(proto); ok {
			return ep, true
		}
	}
	for _, ep := range target.Endpoints {
		p := strings.ToLower(strings.TrimSpace(ep.Protocol))
		if p == ProtocolVNC || p == ProtocolRFB || p == ProtocolWebRTC || p == ProtocolWaylandRFB {
			return ep, true
		}
	}
	return orchestrator.TargetEndpoint{}, false
}

func (i *instance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	i.callMu.Lock()
	defer i.callMu.Unlock()

	i.stateMu.RLock()
	disconnected := i.disconnected
	transport := i.transport
	i.stateMu.RUnlock()

	if disconnected || transport == nil {
		return nil, errors.New("display-interaction connection is disconnected")
	}

	switch method {
	case "hello":
		return json.Marshal(map[string]any{
			"protocol":     "jangolova.display/v1alpha1",
			"adapter":      "display-interaction",
			"capabilities": i.EngineCapabilities(),
		})

	case "capabilities":
		return json.Marshal(map[string]any{
			"capabilities": i.EngineCapabilities(),
		})

	case "describe":
		desc, err := transport.Describe(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(desc)

	case "events":
		return json.Marshal(map[string]any{
			"events": []string{},
		})

	case "health":
		return json.Marshal(map[string]any{
			"status": "ready",
		})

	case "act":
		var req struct {
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, errors.New("invalid action request format")
		}
		return i.dispatchAction(ctx, transport, req.Name, req.Input)

	default:
		return nil, fmt.Errorf("unsupported display-interaction method %q", method)
	}
}

func (i *instance) dispatchAction(ctx context.Context, transport Transport, name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case ActionDisplayDescribe:
		desc, err := transport.Describe(ctx)
		if err != nil {
			i.emitAuditEvent("display.action.denied", name, err.Error())
			return nil, err
		}
		i.emitAuditEvent("display.action.invoked", name, "")
		return json.Marshal(desc)

	case ActionDisplayCapture:
		capture, err := transport.Capture(ctx)
		if err != nil {
			i.emitAuditEvent("display.action.denied", name, err.Error())
			return nil, err
		}
		i.emitAuditEvent("display.action.invoked", name, "")
		return json.Marshal(capture)

	case ActionPointerMove:
		var p struct {
			X int `json:"x"`
			Y int `json:"y"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return nil, errors.New("pointer.move requires x and y integers")
		}
		if err := i.policy.ValidateCoordinates(p.X, p.Y); err != nil {
			i.emitAuditEvent("display.action.denied", name, err.Error())
			return nil, err
		}
		if err := transport.PointerMove(ctx, p.X, p.Y); err != nil {
			i.emitAuditEvent("display.action.failed", name, err.Error())
			return nil, err
		}
		i.emitAuditEvent("display.action.invoked", name, fmt.Sprintf("(%d, %d)", p.X, p.Y))
		return json.Marshal(map[string]any{"status": "ok"})

	case ActionPointerClick:
		var p struct {
			X      int    `json:"x"`
			Y      int    `json:"y"`
			Button string `json:"button"`
			Count  int    `json:"count"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return nil, errors.New("pointer.click requires x and y integers")
		}
		if p.Button == "" {
			p.Button = "left"
		}
		if p.Count <= 0 {
			p.Count = 1
		}
		if err := i.policy.ValidateCoordinates(p.X, p.Y); err != nil {
			i.emitAuditEvent("display.action.denied", name, err.Error())
			return nil, err
		}
		if err := transport.PointerClick(ctx, p.X, p.Y, p.Button, p.Count); err != nil {
			i.emitAuditEvent("display.action.failed", name, err.Error())
			return nil, err
		}
		i.emitAuditEvent("display.action.invoked", name, fmt.Sprintf("(%d, %d) %s", p.X, p.Y, p.Button))
		return json.Marshal(map[string]any{"status": "ok"})

	case ActionPointerDrag:
		var p struct {
			StartX int `json:"startX"`
			StartY int `json:"startY"`
			EndX   int `json:"endX"`
			EndY   int `json:"endY"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return nil, errors.New("pointer.drag requires startX, startY, endX, endY")
		}
		if err := i.policy.ValidateCoordinates(p.StartX, p.StartY); err != nil {
			i.emitAuditEvent("display.action.denied", name, err.Error())
			return nil, err
		}
		if err := i.policy.ValidateCoordinates(p.EndX, p.EndY); err != nil {
			i.emitAuditEvent("display.action.denied", name, err.Error())
			return nil, err
		}
		if err := transport.PointerDrag(ctx, p.StartX, p.StartY, p.EndX, p.EndY); err != nil {
			i.emitAuditEvent("display.action.failed", name, err.Error())
			return nil, err
		}
		i.emitAuditEvent("display.action.invoked", name, fmt.Sprintf("(%d, %d) -> (%d, %d)", p.StartX, p.StartY, p.EndX, p.EndY))
		return json.Marshal(map[string]any{"status": "ok"})

	case ActionPointerScroll:
		var p struct {
			X      int `json:"x"`
			Y      int `json:"y"`
			DeltaX int `json:"deltaX"`
			DeltaY int `json:"deltaY"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return nil, errors.New("pointer.scroll requires x, y, deltaX, deltaY")
		}
		if err := i.policy.ValidateCoordinates(p.X, p.Y); err != nil {
			i.emitAuditEvent("display.action.denied", name, err.Error())
			return nil, err
		}
		if err := transport.PointerScroll(ctx, p.X, p.Y, p.DeltaX, p.DeltaY); err != nil {
			i.emitAuditEvent("display.action.failed", name, err.Error())
			return nil, err
		}
		i.emitAuditEvent("display.action.invoked", name, fmt.Sprintf("(%d, %d) delta (%d, %d)", p.X, p.Y, p.DeltaX, p.DeltaY))
		return json.Marshal(map[string]any{"status": "ok"})

	case ActionKeyboardType:
		var p struct {
			Text      string `json:"text"`
			Sensitive bool   `json:"sensitive"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return nil, errors.New("keyboard.type requires text string")
		}
		if err := i.policy.ValidateText(p.Text); err != nil {
			i.emitAuditEvent("display.action.denied", name, err.Error())
			return nil, err
		}
		if err := transport.KeyboardType(ctx, p.Text); err != nil {
			i.emitAuditEvent("display.action.failed", name, err.Error())
			return nil, err
		}
		loggedText := p.Text
		if p.Sensitive || i.policy.RedactSensitiveInput {
			loggedText = "***REDACTED***"
		}
		i.emitAuditEvent("display.action.invoked", name, loggedText)
		return json.Marshal(map[string]any{"status": "ok"})

	case ActionKeyboardPress:
		var p struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return nil, errors.New("keyboard.press requires key string")
		}
		if err := i.policy.ValidateKey(p.Key); err != nil {
			i.emitAuditEvent("display.action.denied", name, err.Error())
			return nil, err
		}
		if err := transport.KeyboardPress(ctx, p.Key); err != nil {
			i.emitAuditEvent("display.action.failed", name, err.Error())
			return nil, err
		}
		i.emitAuditEvent("display.action.invoked", name, p.Key)
		return json.Marshal(map[string]any{"status": "ok"})

	default:
		return nil, fmt.Errorf("unknown display action %q", name)
	}
}

func (i *instance) emitAuditEvent(eventType, actionName, details string) {
	msg := actionName
	if details != "" {
		msg += ": " + details
	}
	i.emit(orchestrator.EngineEvent{
		Type:       eventType,
		Status:     orchestrator.EngineHealthHealthy,
		Message:    msg,
		OccurredAt: time.Now().UTC(),
	})
}

func (i *instance) Disconnect(ctx context.Context) error {
	i.renewalOnce.Do(func() { close(i.renewalStop) })
	i.renewalWG.Wait()

	i.callMu.Lock()
	i.stateMu.Lock()
	if i.disconnected {
		i.stateMu.Unlock()
		i.callMu.Unlock()
		return nil
	}
	i.disconnected = true
	transport := i.transport
	i.transport = nil
	i.stateMu.Unlock()
	i.callMu.Unlock()

	var err error
	if transport != nil {
		err = transport.Close()
	}
	i.emit(orchestrator.EngineEvent{
		Type:       "display.disconnected",
		Status:     orchestrator.EngineHealthStopped,
		OccurredAt: time.Now().UTC(),
	})
	close(i.events)
	return err
}

func (i *instance) Authorize(ctx context.Context, request orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	action := strings.TrimSpace(request.Action)
	if action == "" {
		return orchestrator.AuthorizeDecision{Authorized: false}, errors.New("display interaction action name is required")
	}
	if err := i.policy.authorize(action, request.Input); err != nil {
		return orchestrator.AuthorizeDecision{Authorized: false}, err
	}
	return orchestrator.AuthorizeDecision{Authorized: true}, nil
}

func (i *instance) EngineCapabilities() []string {
	i.stateMu.RLock()
	defer i.stateMu.RUnlock()
	return append([]string(nil), i.capabilities...)
}

func (i *instance) EngineEvents() <-chan orchestrator.EngineEvent {
	return i.events
}

func (i *instance) EngineHealth(ctx context.Context) orchestrator.EngineHealth {
	i.stateMu.RLock()
	disconnected := i.disconnected
	i.stateMu.RUnlock()

	if disconnected {
		return orchestrator.EngineHealth{
			Status:     orchestrator.EngineHealthStopped,
			Message:    "display interaction disconnected",
			ObservedAt: time.Now().UTC(),
		}
	}
	return orchestrator.EngineHealth{
		Status:     orchestrator.EngineHealthHealthy,
		Message:    "display interaction active",
		ObservedAt: time.Now().UTC(),
	}
}

func (i *instance) emit(event orchestrator.EngineEvent) {
	select {
	case i.events <- event:
	default:
	}
}

// defaultConnector provides reference in-memory transport handling for VNC/WebRTC display targets.
type defaultConnector struct{}

func (d defaultConnector) Connect(_ context.Context, endpoint orchestrator.TargetEndpoint) (Transport, error) {
	proto := strings.ToLower(strings.TrimSpace(endpoint.Protocol))
	return &referenceTransport{
		protocol: proto,
		desc: DisplayDescription{
			Width:       1920,
			Height:      1080,
			ColorDepth:  24,
			Title:       "Display Target (" + endpoint.URL + ")",
			Focused:     true,
			Orientation: "landscape",
		},
	}, nil
}

type referenceTransport struct {
	mu       sync.Mutex
	protocol string
	desc     DisplayDescription
	curX     int
	curY     int
}

func (r *referenceTransport) Protocol() string {
	return r.protocol
}

func (r *referenceTransport) Describe(_ context.Context) (DisplayDescription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.desc, nil
}

func (r *referenceTransport) Capture(_ context.Context) (FrameCapture, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dummyPixel := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	return FrameCapture{
		Format:     "image/png",
		Width:      r.desc.Width,
		Height:     r.desc.Height,
		Base64Data: dummyPixel,
	}, nil
}

func (r *referenceTransport) PointerMove(_ context.Context, x, y int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.curX = x
	r.curY = y
	return nil
}

func (r *referenceTransport) PointerClick(_ context.Context, x, y int, _ string, _ int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.curX = x
	r.curY = y
	return nil
}

func (r *referenceTransport) PointerDrag(_ context.Context, _, _, endX, endY int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.curX = endX
	r.curY = endY
	return nil
}

func (r *referenceTransport) PointerScroll(_ context.Context, x, y, _, _ int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.curX = x
	r.curY = y
	return nil
}

func (r *referenceTransport) KeyboardType(_ context.Context, text string) error {
	if text == "" {
		return errors.New("keyboard text cannot be empty")
	}
	return nil
}

func (r *referenceTransport) KeyboardPress(_ context.Context, key string) error {
	if key == "" {
		return errors.New("key string cannot be empty")
	}
	return nil
}

func (r *referenceTransport) Close() error {
	return nil
}

// Dummy base64 validation helper to keep package clean.
func _() {
	_, _ = base64.StdEncoding.DecodeString("")
}
