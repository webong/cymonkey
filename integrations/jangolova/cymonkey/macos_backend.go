package cymonkey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"jangolova/internal/bridge"
	contract "jangolova/internal/cymonkey"
	"jangolova/internal/manifest"
	"jangolova/internal/orchestrator"
)

type macOSCooperativeBackend struct{}

func (macOSCooperativeBackend) Name() BackendName       { return BackendMacOSCooperative }
func (macOSCooperativeBackend) Domain() contract.Domain { return contract.DomainComputer }
func (macOSCooperativeBackend) Compatible(target orchestrator.EngineTarget) bool {
	return target.Kind == "macos-application"
}

func (macOSCooperativeBackend) Connect(
	_ context.Context,
	spec manifest.EngineSpec,
	target orchestrator.EngineTarget,
	config options,
) (orchestrator.EngineInstance, error) {
	if target.Kind != "macos-application" {
		return nil, errors.New("Cymonkey macOS backend requires target.kind macos-application")
	}
	host, err := bridge.NewWebSocketHost(config.Native.ControlListen)
	if err != nil {
		return nil, fmt.Errorf("create Cymonkey macOS control host: %w", err)
	}
	running := &macOSInstance{
		host: host, policy: config.Policy,
		required: stableStrings(spec.RequiredCapabilities),
		events:   make(chan orchestrator.EngineEvent, 8),
	}
	running.emit(orchestrator.EngineEvent{
		Type: "cymonkey.macos.awaiting_helper", Status: orchestrator.EngineHealthStarting,
		OccurredAt: time.Now().UTC(),
	})
	return running, nil
}

type macOSInstance struct {
	host            *bridge.WebSocketHost
	policy          policyOptions
	required        []string
	connectMu       sync.Mutex
	stateMu         sync.RWMutex
	connection      *bridge.WebSocketConnection
	capabilities    []contract.Capability
	capabilityNames []string
	closed          bool
	events          chan orchestrator.EngineEvent
	eventsOnce      sync.Once
	eventsMu        sync.RWMutex
	eventsClosed    bool
}

var _ orchestrator.EngineInstance = (*macOSInstance)(nil)
var _ orchestrator.EngineHealthProvider = (*macOSInstance)(nil)
var _ orchestrator.EngineCapabilityProvider = (*macOSInstance)(nil)
var _ orchestrator.EngineEventSource = (*macOSInstance)(nil)
var _ orchestrator.EngineCallerLaunchProvider = (*macOSInstance)(nil)
var _ bridge.WebSocketHostProvider = (*macOSInstance)(nil)
var _ bridge.Caller = (*macOSInstance)(nil)

func (i *macOSInstance) BridgeWebSocketHost() *bridge.WebSocketHost { return i.host }

func (i *macOSInstance) EngineCallerLaunch() orchestrator.CallerLaunch {
	return orchestrator.CallerLaunch{Environment: map[string]string{
		"JANGOLOVA_CYMONKEY_CONTROL_URL":   i.host.Endpoint(),
		"JANGOLOVA_CYMONKEY_CONTROL_TOKEN": i.host.Token(),
		"JANGOLOVA_CYMONKEY_PROTOCOL":      contract.ProtocolVersion,
	}}
}

func (i *macOSInstance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	connection, err := i.ensureConnected(ctx)
	if err != nil {
		return nil, err
	}
	switch method {
	case bridge.MethodHello, bridge.MethodDescribe, bridge.MethodEvents:
		return connection.Call(ctx, method, params)
	case bridge.MethodCapabilities:
		i.stateMu.RLock()
		defer i.stateMu.RUnlock()
		return json.Marshal(i.capabilities)
	case bridge.MethodAct:
		action, err := decodeAction(params)
		if err != nil {
			return nil, fmt.Errorf("decode Cymonkey macOS action: %w", err)
		}
		if !capabilityAllowed(i.policy.AllowedCapabilities, action.Name) || !i.advertises(action.Name) {
			return nil, fmt.Errorf("Cymonkey policy denied capability %q", action.Name)
		}
		if !bundleAllowedForMacOSAction(i.policy.AllowedBundleIDs, action.Input) {
			return nil, errors.New("Cymonkey policy denied the macOS application surface")
		}
		return connection.Call(ctx, method, params)
	default:
		return nil, fmt.Errorf("unsupported Cymonkey method %q", method)
	}
}

func (i *macOSInstance) ensureConnected(ctx context.Context) (*bridge.WebSocketConnection, error) {
	i.stateMu.RLock()
	if i.closed {
		i.stateMu.RUnlock()
		return nil, errors.New("Cymonkey macOS control host is disconnected")
	}
	if i.connection != nil {
		connection := i.connection
		i.stateMu.RUnlock()
		return connection, nil
	}
	i.stateMu.RUnlock()

	i.connectMu.Lock()
	defer i.connectMu.Unlock()
	i.stateMu.RLock()
	if i.connection != nil {
		connection := i.connection
		i.stateMu.RUnlock()
		return connection, nil
	}
	i.stateMu.RUnlock()
	connection, err := i.host.WaitConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("wait for caller-owned Cymonkey macOS helper: %w", err)
	}
	capabilities, err := i.handshake(ctx, connection)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	names := []string{"act", "capabilities", "describe", "events", "target.macos-cooperative"}
	for _, capability := range capabilities {
		names = append(names, capability.Name)
	}
	names = stableStrings(names)
	if missing := missingCapabilities(i.required, names); len(missing) != 0 {
		_ = connection.Close()
		return nil, fmt.Errorf("Cymonkey macOS helper is missing required capabilities: %s", strings.Join(missing, ", "))
	}
	i.stateMu.Lock()
	if i.closed {
		i.stateMu.Unlock()
		_ = connection.Close()
		return nil, errors.New("Cymonkey macOS control host is disconnected")
	}
	i.connection, i.capabilities, i.capabilityNames = connection, capabilities, names
	i.stateMu.Unlock()
	i.emit(orchestrator.EngineEvent{Type: "cymonkey.macos.helper_connected", Status: orchestrator.EngineHealthHealthy, OccurredAt: time.Now().UTC()})
	return connection, nil
}

func (i *macOSInstance) handshake(ctx context.Context, connection *bridge.WebSocketConnection) ([]contract.Capability, error) {
	rawHello, err := connection.Call(ctx, bridge.MethodHello, json.RawMessage(`{}`))
	if err != nil {
		return nil, fmt.Errorf("Cymonkey macOS helper hello: %w", err)
	}
	var hello contract.Hello
	if err := json.Unmarshal(rawHello, &hello); err != nil || contract.ValidateHello(hello) != nil || !containsDomain(hello.Domains, contract.DomainComputer) || !containsString(hello.Runtimes, "macos-app") {
		return nil, errors.New("Cymonkey macOS helper returned an incompatible hello")
	}
	rawCapabilities, err := connection.Call(ctx, bridge.MethodCapabilities, json.RawMessage(`{}`))
	if err != nil {
		return nil, fmt.Errorf("Cymonkey macOS helper capabilities: %w", err)
	}
	var capabilities []contract.Capability
	if err := json.Unmarshal(rawCapabilities, &capabilities); err != nil {
		return nil, errors.New("Cymonkey macOS helper returned invalid capabilities")
	}
	if err := contract.ValidateCapabilities(capabilities); err != nil {
		return nil, fmt.Errorf("Cymonkey macOS helper capabilities: %w", err)
	}
	filtered := capabilities[:0]
	for _, capability := range capabilities {
		if capabilityAllowed(i.policy.AllowedCapabilities, capability.Name) {
			filtered = append(filtered, capability)
		}
	}
	sort.Slice(filtered, func(left, right int) bool { return filtered[left].Name < filtered[right].Name })
	return filtered, nil
}

func (i *macOSInstance) advertises(name string) bool {
	i.stateMu.RLock()
	defer i.stateMu.RUnlock()
	for _, capability := range i.capabilities {
		if capability.Name == name {
			return true
		}
	}
	return false
}

func (i *macOSInstance) Disconnect(ctx context.Context) error {
	i.stateMu.Lock()
	if i.closed {
		i.stateMu.Unlock()
		return nil
	}
	i.closed = true
	i.connection = nil
	i.stateMu.Unlock()
	err := i.host.Close(ctx)
	i.finish(orchestrator.EngineEvent{Type: "cymonkey.macos.disconnected", Status: orchestrator.EngineHealthStopped, OccurredAt: time.Now().UTC()})
	return err
}

func (i *macOSInstance) EngineHealth(context.Context) orchestrator.EngineHealth {
	i.stateMu.RLock()
	defer i.stateMu.RUnlock()
	status, message := orchestrator.EngineHealthStarting, "waiting for caller-owned macOS helper"
	if i.closed {
		status, message = orchestrator.EngineHealthStopped, "Cymonkey macOS control host is disconnected"
	} else if i.connection != nil {
		status, message = orchestrator.EngineHealthHealthy, "caller-owned macOS helper is connected"
	}
	return orchestrator.EngineHealth{Status: status, Message: message, ObservedAt: time.Now().UTC()}
}

func (i *macOSInstance) Authorize(ctx context.Context, request orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	action := strings.TrimSpace(request.Action)
	if action == "" {
		return orchestrator.AuthorizeDecision{Authorized: false}, errors.New("Cymonkey macOS action name is required")
	}
	if !capabilityAllowed(i.policy.AllowedCapabilities, action) {
		return orchestrator.AuthorizeDecision{Authorized: false, Reason: fmt.Sprintf("Cymonkey policy denied capability %q", action)}, nil
	}
	i.stateMu.RLock()
	advertised := false
	for _, name := range i.capabilityNames {
		if name == action {
			advertised = true
			break
		}
	}
	i.stateMu.RUnlock()
	if !advertised {
		return orchestrator.AuthorizeDecision{Authorized: false, Reason: fmt.Sprintf("Cymonkey action %q was not advertised by macOS helper", action)}, nil
	}
	return orchestrator.AuthorizeDecision{Authorized: true}, nil
}

func (i *macOSInstance) EngineCapabilities() []string {
	i.stateMu.RLock()
	defer i.stateMu.RUnlock()
	if len(i.capabilityNames) == 0 {
		return []string{"act", "capabilities", "describe", "events", "target.macos-cooperative"}
	}
	return append([]string(nil), i.capabilityNames...)
}

func (i *macOSInstance) EngineEvents() <-chan orchestrator.EngineEvent { return i.events }

func (i *macOSInstance) emit(event orchestrator.EngineEvent) {
	i.eventsMu.RLock()
	defer i.eventsMu.RUnlock()
	if i.eventsClosed {
		return
	}
	select {
	case i.events <- event:
	default:
	}
}

func (i *macOSInstance) finish(event orchestrator.EngineEvent) {
	i.eventsOnce.Do(func() {
		i.eventsMu.Lock()
		defer i.eventsMu.Unlock()
		if i.eventsClosed {
			return
		}
		select {
		case i.events <- event:
		default:
		}
		i.eventsClosed = true
		close(i.events)
	})
}

func containsDomain(values []contract.Domain, expected contract.Domain) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func bundleAllowedForMacOSAction(allowed []string, input map[string]any) bool {
	if len(allowed) == 0 {
		return true
	}
	surfaceID, _ := input["surfaceId"].(string)
	for _, bundleID := range allowed {
		if strings.HasPrefix(surfaceID, "macos:"+bundleID+":") {
			return true
		}
	}
	return false
}
