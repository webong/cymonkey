package cymonkey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cymonkey/internal/bridge"
	contract "cymonkey/internal/cymonkey"
	"cymonkey/internal/manifest"
	"cymonkey/internal/orchestrator"
	"cymonkey/targetconn"
)

type enginePresentationBackend struct{}

var _ bridge.Caller = (*engineInstance)(nil)

func (enginePresentationBackend) Name() BackendName { return BackendName("engine-native") }

func (enginePresentationBackend) Domains() []contract.Domain {
	return []contract.Domain{contract.DomainRender}
}

func (enginePresentationBackend) Compatible(target orchestrator.EngineTarget) bool {
	if target.Kind != "native-presentation" && target.Kind != "unity" && target.Kind != "unreal" && target.Kind != "godot" {
		return false
	}
	_, ok := target.Endpoint("websocket")
	return ok
}

func (b enginePresentationBackend) Connect(ctx context.Context, spec manifest.EngineSpec, target orchestrator.EngineTarget, config options) (orchestrator.EngineInstance, error) {
	if !b.Compatible(target) {
		return nil, errors.New("Cymonkey render domain requires target.kind native-presentation, unity, unreal, or godot with a websocket endpoint")
	}
	endpoint, ok := target.Endpoint("websocket")
	if !ok {
		return nil, errors.New("Cymonkey engine backend requires a caller-owned websocket endpoint")
	}
	if err := targetconn.Validate(endpoint); err != nil {
		return nil, err
	}

	connector := EngineWebSocketConnector{}
	transport, err := connector.Connect(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("connect Cymonkey engine backend transport: %w", err)
	}

	// Validate Cymonkey v1alpha2 engine hello handshake
	helloRaw, err := transport.Call(ctx, "hello", json.RawMessage(`{}`))
	if err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("Cymonkey engine handshake failed: %w", err)
	}
	var hello contract.Hello
	if err := json.Unmarshal(helloRaw, &hello); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("decode Cymonkey engine hello response: %w", err)
	}
	if hello.ProtocolVersion != contract.ProtocolVersion || !containsDomain(hello.Domains, contract.DomainRender) {
		_ = transport.Close()
		return nil, fmt.Errorf("Cymonkey render peer advertises incompatible protocol or domain")
	}

	// Validate capabilities
	capsRaw, err := transport.Call(ctx, "capabilities", json.RawMessage(`{}`))
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
		transport:    transport,
		endpoint:     endpoint,
		capabilities: capNames,
		events:       make(chan orchestrator.EngineEvent, 8),
		renewalStop:  make(chan struct{}),
	}
	running.emit(orchestrator.EngineEvent{Type: "cymonkey.connected", Status: "connected", OccurredAt: time.Now().UTC()})

	return running, nil
}

type engineInstance struct {
	transport    EngineTransport
	endpoint     orchestrator.TargetEndpoint
	capabilities []string
	events       chan orchestrator.EngineEvent
	renewalStop  chan struct{}
	closed       bool
}

func (i *engineInstance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	return i.transport.Call(ctx, method, params)
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
