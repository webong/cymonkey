package jangolova

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"jangolova/contract"
	"jangolova/sdk"
)

type enginePresentationBackend struct{}

func (enginePresentationBackend) Name() BackendName { return BackendName("engine-native") }
func (enginePresentationBackend) Domains() []contract.Domain {
	return []contract.Domain{contract.DomainRender}
}
func (enginePresentationBackend) Compatible(target sdk.EngineTarget) bool {
	switch target.Kind {
	case "native-presentation", "unity", "unreal", "godot", "blender":
	default:
		return false
	}
	_, ok := target.Endpoint("websocket")
	return ok
}
func (b enginePresentationBackend) Connect(ctx context.Context, spec sdk.EngineSpec, target sdk.EngineTarget, config Options) (sdk.EngineInstance, error) {
	if !b.Compatible(target) {
		return nil, errors.New("render module requires a caller-owned WebSocket presentation endpoint")
	}
	endpoint, _ := target.Endpoint("websocket")
	if err := config.Host.Validate(endpoint); err != nil {
		return nil, err
	}
	transport, err := (EngineWebSocketConnector{Host: config.Host}).Connect(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (sdk.EngineInstance, error) { _ = transport.Close(); return nil, err }
	raw, err := transport.Call(ctx, "hello", json.RawMessage(`{}`))
	if err != nil {
		return fail(err)
	}
	var hello contract.Hello
	if err := json.Unmarshal(raw, &hello); err != nil {
		return fail(errors.New("invalid render hello JSON"))
	}
	if err := contract.ValidateHello(hello); err != nil {
		return fail(err)
	}
	if !containsDomain(hello.Domains, contract.DomainRender) {
		return fail(errors.New("peer does not advertise the render domain"))
	}
	expected := target.Kind
	if expected == "native-presentation" {
		expected = target.Metadata["runtime"]
	}
	if expected != "" && !containsString(hello.Runtimes, expected) {
		return fail(errors.New("peer runtime does not match the selected target"))
	}
	raw, err = transport.Call(ctx, "capabilities", json.RawMessage(`{}`))
	if err != nil {
		return fail(err)
	}
	var capabilities []contract.Capability
	if err := json.Unmarshal(raw, &capabilities); err != nil {
		return fail(errors.New("invalid render capabilities JSON"))
	}
	if err := contract.ValidateCapabilities(capabilities); err != nil {
		return fail(err)
	}
	selected := make([]contract.Capability, 0, len(capabilities))
	names := []string{}
	for _, c := range capabilities {
		if c.Domain != contract.DomainRender || !containsString(hello.Runtimes, c.Runtime) {
			return fail(errors.New("capability is outside the negotiated runtime/domain"))
		}
		if expected != "" && c.Runtime != expected {
			continue
		}
		if capabilityAllowed(config.Policy.AllowedCapabilities, c.Name) {
			selected = append(selected, c)
			names = append(names, c.Name)
		}
	}
	if missing := missingCapabilities(spec.RequiredCapabilities, names); len(missing) > 0 {
		return fail(fmt.Errorf("render peer is missing required capabilities: %v", missing))
	}
	running := &engineInstance{transport: transport, endpoint: endpoint, host: config.Host, capabilities: names, descriptors: selected, events: make(chan sdk.EngineEvent, 8)}
	running.events <- sdk.EngineEvent{Type: "jangolova.connected", Status: sdk.EngineHealthHealthy, OccurredAt: time.Now().UTC()}
	return running, nil
}

type engineInstance struct {
	mu           sync.Mutex
	transport    EngineTransport
	endpoint     sdk.TargetEndpoint
	host         sdk.Host
	capabilities []string
	descriptors  []contract.Capability
	events       chan sdk.EngineEvent
	closed       bool
}

func (i *engineInstance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil, errors.New("Jangolova module is disconnected")
	}
	if err := i.host.Validate(i.endpoint); err != nil {
		return nil, err
	}
	switch method {
	case "hello", "describe", "health", "events":
	case "capabilities":
		return json.Marshal(i.descriptors)
	case "act":
		var action contract.Action
		if json.Unmarshal(params, &action) != nil || action.Name == "" {
			return nil, errors.New("invalid action request")
		}
		found := false
		for _, c := range i.descriptors {
			if c.Name == action.Name && (action.Domain == "" || action.Domain == c.Domain) && (action.Runtime == "" || action.Runtime == c.Runtime) {
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("action was not advertised and allowlisted for this module")
		}
	default:
		return nil, fmt.Errorf("unsupported Jangolova method %q", method)
	}
	return i.transport.Call(ctx, method, params)
}
func (i *engineInstance) Authorize(_ context.Context, r sdk.AuthorizeRequest) (sdk.AuthorizeDecision, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || !containsString(i.capabilities, r.Action) {
		return sdk.AuthorizeDecision{Reason: "action is unavailable"}, nil
	}
	return sdk.AuthorizeDecision{Authorized: true}, nil
}
func (i *engineInstance) Disconnect(context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil
	}
	i.closed = true
	err := i.transport.Close()
	select {
	case i.events <- sdk.EngineEvent{Type: "jangolova.disconnected", Status: sdk.EngineHealthStopped, OccurredAt: time.Now().UTC()}:
	default:
	}
	close(i.events)
	return err
}
func (i *engineInstance) EngineCapabilities() []string {
	return append([]string(nil), i.capabilities...)
}
func (i *engineInstance) EngineEvents() <-chan sdk.EngineEvent { return i.events }
func (i *engineInstance) EngineHealth(ctx context.Context) sdk.EngineHealth {
	raw, err := i.Call(ctx, "health", json.RawMessage(`{}`))
	health := sdk.EngineHealth{Status: sdk.EngineHealthUnhealthy, ObservedAt: time.Now().UTC()}
	if err != nil {
		health.Message = err.Error()
		return health
	}
	var result struct {
		Status    string `json:"status"`
		Connected bool   `json:"connected"`
	}
	if json.Unmarshal(raw, &result) == nil && (result.Connected || result.Status == "ready" || result.Status == "healthy" || result.Status == "connected") {
		health.Status = sdk.EngineHealthHealthy
	}
	return health
}
