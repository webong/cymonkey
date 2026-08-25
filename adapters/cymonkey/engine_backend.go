package cymonkey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	pacmanadapter "jangolova/adapters/pacman"
	"jangolova/internal/bridge"
	contract "jangolova/internal/cymonkey"
	"jangolova/internal/manifest"
	"jangolova/internal/orchestrator"
	"jangolova/targetconn"
)

// pacmanProtocolVersion is the legacy engine wire protocol accepted through
// the translation adapter.
const pacmanProtocolVersion = "jangolova.pacman/v1alpha1"

type enginePresentationBackend struct{}

var _ bridge.Caller = (*engineInstance)(nil)

func (enginePresentationBackend) Name() BackendName { return BackendName("engine-pacman") }

func (enginePresentationBackend) Profile() contract.Profile { return contract.ProfileEngine }

func (enginePresentationBackend) Compatible(target orchestrator.EngineTarget) bool {
	if target.Kind != "native-presentation" && target.Kind != "unity" && target.Kind != "unreal" && target.Kind != "godot" {
		return false
	}
	_, okWS := target.Endpoint("websocket")
	_, okPacman := target.Endpoint("pacman-ws")
	return okWS || okPacman
}

func (b enginePresentationBackend) Connect(ctx context.Context, spec manifest.EngineSpec, target orchestrator.EngineTarget, config options) (orchestrator.EngineInstance, error) {
	if !b.Compatible(target) {
		return nil, errors.New("Cymonkey engine profile requires target.kind native-presentation, unity, unreal, or godot with a websocket or pacman-ws endpoint")
	}
	endpointProtocol := "pacman-ws"
	endpoint, ok := target.Endpoint(endpointProtocol)
	if !ok {
		endpointProtocol = "websocket"
		endpoint, ok = target.Endpoint(endpointProtocol)
	}
	if !ok {
		return nil, errors.New("Cymonkey engine backend requires a caller-owned websocket or pacman-ws endpoint")
	}
	if err := targetconn.Validate(endpoint); err != nil {
		return nil, err
	}

	adapter := pacmanadapter.WebSocketConnector{}
	transport, err := adapter.Connect(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("connect Cymonkey engine backend transport: %w", err)
	}

	caller, err := bindEngineCaller(ctx, transport)
	if err != nil {
		return nil, err
	}

	// Validate Cymonkey v1alpha2 engine hello handshake via caller
	helloRaw, err := caller.Call(ctx, "hello", json.RawMessage(`{}`))
	if err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("Cymonkey engine handshake failed: %w", err)
	}
	var hello contract.Hello
	if err := json.Unmarshal(helloRaw, &hello); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("decode Cymonkey engine hello response: %w", err)
	}

	// Validate capabilities
	capsRaw, err := caller.Call(ctx, "capabilities", json.RawMessage(`{}`))
	if err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("Cymonkey engine capabilities call failed: %w", err)
	}
	var capabilities []contract.Capability
	if err := json.Unmarshal(capsRaw, &capabilities); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("decode Cymonkey engine capabilities response: %w", err)
	}

	capNames := make([]string, 0, len(capabilities))
	for _, c := range capabilities {
		capNames = append(capNames, c.Name)
	}

	running := &engineInstance{
		caller:       caller,
		transport:    transport,
		endpoint:     endpoint,
		capabilities: capNames,
		events:       make(chan orchestrator.EngineEvent, 8),
		renewalStop:  make(chan struct{}),
	}
	running.emit(orchestrator.EngineEvent{Type: "cymonkey.connected", Status: "connected", OccurredAt: time.Now().UTC()})

	return running, nil
}

// bindEngineCaller negotiates the wire protocol during the raw hello exchange:
// native cymonkey/v1alpha2 peers are passed through untouched while legacy
// pacman/v1alpha1 peers are wrapped in the translation adapter.
func bindEngineCaller(ctx context.Context, transport pacmanadapter.Transport) (bridge.Caller, error) {
	raw, err := transport.Call(ctx, "hello", json.RawMessage(`{}`))
	if err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("Cymonkey engine hello probe failed: %w", err)
	}
	var probe struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("decode Cymonkey engine hello protocol: %w", err)
	}
	switch probe.ProtocolVersion {
	case contract.ProtocolVersion:
		return transport, nil
	case pacmanProtocolVersion:
		return contract.NewPacmanAdapter(transport), nil
	default:
		_ = transport.Close()
		return nil, fmt.Errorf("Cymonkey engine peer advertises unsupported protocol %q", probe.ProtocolVersion)
	}
}

type engineInstance struct {
	caller       bridge.Caller
	transport    pacmanadapter.Transport
	endpoint     orchestrator.TargetEndpoint
	capabilities []string
	events       chan orchestrator.EngineEvent
	renewalStop  chan struct{}
	closed       bool
}

func (i *engineInstance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	return i.caller.Call(ctx, method, params)
}

func (i *engineInstance) Authorize(ctx context.Context, request orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	return orchestrator.AuthorizeDecision{Authorized: true}, nil
}

func (i *engineInstance) Disconnect(ctx context.Context) error {
	if i.closed {
		return nil
	}
	i.closed = true
	err := i.transport.Close()
	i.emit(orchestrator.EngineEvent{Type: "cymonkey.disconnected", Status: "stopped", OccurredAt: time.Now().UTC()})
	return err
}

func (i *engineInstance) EngineCapabilities() []string {
	return append([]string(nil), i.capabilities...)
}

func (i *engineInstance) EngineEvents() <-chan orchestrator.EngineEvent {
	return i.events
}

func (i *engineInstance) emit(event orchestrator.EngineEvent) {
	select {
	case i.events <- event:
	default:
	}
}
