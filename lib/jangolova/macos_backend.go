package jangolova

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	contract "jangolova/contract"
	"jangolova/sdk"
)

type macOSCooperativeBackend struct{}

func (macOSCooperativeBackend) Name() BackendName { return BackendMacOSCooperative }
func (macOSCooperativeBackend) Domains() []contract.Domain {
	return []contract.Domain{contract.DomainViewer}
}
func (macOSCooperativeBackend) Compatible(target sdk.EngineTarget) bool {
	return target.Kind == "macos-application"
}

func (macOSCooperativeBackend) Connect(
	_ context.Context,
	spec sdk.EngineSpec,
	target sdk.EngineTarget,
	config Options,
) (sdk.EngineInstance, error) {
	if target.Kind != "macos-application" {
		return nil, errors.New("Jangolova macOS backend requires target.kind macos-application")
	}
	if config.Host.ListenWebSocket == nil {
		return nil, errors.New("Jangolova host cooperative listener is required")
	}
	host, err := config.Host.ListenWebSocket(config.Native.ControlListen)
	if err != nil {
		return nil, fmt.Errorf("create Jangolova macOS control host: %w", err)
	}
	running := &macOSInstance{
		host: host, policy: config.Policy,
		required: stableStrings(spec.RequiredCapabilities),
		events:   make(chan sdk.EngineEvent, 8),
		runtime:  "macos-app", targetCapability: "target.macos-cooperative",
	}
	running.emit(sdk.EngineEvent{
		Type: "jangolova.macos.awaiting_helper", Status: sdk.EngineHealthStarting,
		OccurredAt: time.Now().UTC(),
	})
	return running, nil
}

type macOSInstance struct {
	host             sdk.Listener
	policy           PolicyLimits
	required         []string
	runtime          string
	targetCapability string
	connectMu        sync.Mutex
	stateMu          sync.RWMutex
	connection       sdk.Transport
	capabilities     []contract.Capability
	capabilityNames  []string
	closed           bool
	events           chan sdk.EngineEvent
	eventsOnce       sync.Once
	eventsMu         sync.RWMutex
	eventsClosed     bool
}

var _ sdk.EngineInstance = (*macOSInstance)(nil)
var _ sdk.EngineHealthProvider = (*macOSInstance)(nil)
var _ sdk.EngineCapabilityProvider = (*macOSInstance)(nil)
var _ sdk.EngineEventSource = (*macOSInstance)(nil)
var _ sdk.EngineCallerLaunchProvider = (*macOSInstance)(nil)
var _ sdk.WebSocketHostProvider = (*macOSInstance)(nil)
var _ sdk.Caller = (*macOSInstance)(nil)

func (i *macOSInstance) BridgeWebSocketHost() sdk.Listener { return i.host }

func (i *macOSInstance) EngineCallerLaunch() sdk.CallerLaunch {
	return sdk.CallerLaunch{Environment: map[string]string{
		"JANGOLOVA_CONTROL_URL":      i.host.Endpoint(),
		"JANGOLOVA_CONTROL_TOKEN":    i.host.Token(),
		"JANGOLOVA_CONTROL_PROTOCOL": contract.ProtocolVersion,
	}}
}

func (i *macOSInstance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	connection, err := i.ensureConnected(ctx)
	if err != nil {
		return nil, err
	}
	switch method {
	case "health":
		health := i.EngineHealth(ctx)
		return json.Marshal(map[string]any{"status": health.Status, "observedAt": health.ObservedAt})
	case sdk.MethodHello, sdk.MethodDescribe, sdk.MethodEvents:
		return connection.Call(ctx, method, params)
	case sdk.MethodCapabilities:
		i.stateMu.RLock()
		defer i.stateMu.RUnlock()
		return json.Marshal(i.capabilities)
	case sdk.MethodAct:
		action, err := decodeAction(params)
		if err != nil {
			return nil, fmt.Errorf("decode Jangolova macOS action: %w", err)
		}
		if !capabilityAllowed(i.policy.AllowedCapabilities, action.Name) || !i.advertises(action.Name) {
			return nil, fmt.Errorf("Jangolova policy denied capability %q", action.Name)
		}
		if i.runtime == "macos-app" && !bundleAllowedForMacOSAction(i.policy.AllowedBundleIDs, action.Input) {
			return nil, errors.New("Jangolova policy denied the macOS application surface")
		}
		return connection.Call(ctx, method, params)
	default:
		return nil, fmt.Errorf("unsupported Jangolova method %q", method)
	}
}

func (i *macOSInstance) ensureConnected(ctx context.Context) (sdk.Transport, error) {
	i.stateMu.RLock()
	if i.closed {
		i.stateMu.RUnlock()
		return nil, errors.New("Jangolova macOS control host is disconnected")
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
		return nil, fmt.Errorf("wait for caller-owned Jangolova macOS helper: %w", err)
	}
	capabilities, err := i.handshake(ctx, connection)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	names := []string{"act", "capabilities", "describe", "events", i.targetCapability}
	for _, capability := range capabilities {
		names = append(names, capability.Name)
	}
	names = stableStrings(names)
	if missing := missingCapabilities(i.required, names); len(missing) != 0 {
		_ = connection.Close()
		return nil, fmt.Errorf("Jangolova macOS helper is missing required capabilities: %s", strings.Join(missing, ", "))
	}
	i.stateMu.Lock()
	if i.closed {
		i.stateMu.Unlock()
		_ = connection.Close()
		return nil, errors.New("Jangolova macOS control host is disconnected")
	}
	i.connection, i.capabilities, i.capabilityNames = connection, capabilities, names
	i.stateMu.Unlock()
	i.emit(sdk.EngineEvent{Type: "jangolova." + i.platform() + ".helper_connected", Status: sdk.EngineHealthHealthy, OccurredAt: time.Now().UTC()})
	return connection, nil
}

func (i *macOSInstance) handshake(ctx context.Context, connection sdk.Transport) ([]contract.Capability, error) {
	rawHello, err := connection.Call(ctx, sdk.MethodHello, json.RawMessage(`{}`))
	if err != nil {
		return nil, fmt.Errorf("Jangolova macOS helper hello: %w", err)
	}
	var hello contract.Hello
	if err := json.Unmarshal(rawHello, &hello); err != nil || contract.ValidateHello(hello) != nil || !containsDomain(hello.Domains, contract.DomainViewer) || !containsString(hello.Runtimes, i.runtime) {
		return nil, errors.New("Jangolova macOS helper returned an incompatible hello")
	}
	rawCapabilities, err := connection.Call(ctx, sdk.MethodCapabilities, json.RawMessage(`{}`))
	if err != nil {
		return nil, fmt.Errorf("Jangolova macOS helper capabilities: %w", err)
	}
	var capabilities []contract.Capability
	if err := json.Unmarshal(rawCapabilities, &capabilities); err != nil {
		return nil, errors.New("Jangolova macOS helper returned invalid capabilities")
	}
	if err := contract.ValidateCapabilities(capabilities); err != nil {
		return nil, fmt.Errorf("Jangolova macOS helper capabilities: %w", err)
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
	i.finish(sdk.EngineEvent{Type: "jangolova." + i.platform() + ".disconnected", Status: sdk.EngineHealthStopped, OccurredAt: time.Now().UTC()})
	return err
}

func (i *macOSInstance) EngineHealth(context.Context) sdk.EngineHealth {
	i.stateMu.RLock()
	defer i.stateMu.RUnlock()
	status, message := sdk.EngineHealthStarting, "waiting for caller-owned "+i.platform()+" helper"
	if i.closed {
		status, message = sdk.EngineHealthStopped, "Jangolova "+i.platform()+" control host is disconnected"
	} else if i.connection != nil {
		status, message = sdk.EngineHealthHealthy, "caller-owned "+i.platform()+" helper is connected"
	}
	return sdk.EngineHealth{Status: status, Message: message, ObservedAt: time.Now().UTC()}
}

func (i *macOSInstance) Authorize(ctx context.Context, request sdk.AuthorizeRequest) (sdk.AuthorizeDecision, error) {
	action := strings.TrimSpace(request.Action)
	if action == "" {
		return sdk.AuthorizeDecision{Authorized: false}, errors.New("Jangolova macOS action name is required")
	}
	if !capabilityAllowed(i.policy.AllowedCapabilities, action) {
		return sdk.AuthorizeDecision{Authorized: false, Reason: fmt.Sprintf("Jangolova policy denied capability %q", action)}, nil
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
		return sdk.AuthorizeDecision{Authorized: false, Reason: fmt.Sprintf("Jangolova action %q was not advertised by macOS helper", action)}, nil
	}
	return sdk.AuthorizeDecision{Authorized: true}, nil
}

func (i *macOSInstance) EngineCapabilities() []string {
	i.stateMu.RLock()
	defer i.stateMu.RUnlock()
	if len(i.capabilityNames) == 0 {
		return []string{"act", "capabilities", "describe", "events", i.targetCapability}
	}
	return append([]string(nil), i.capabilityNames...)
}

func (i *macOSInstance) EngineEvents() <-chan sdk.EngineEvent { return i.events }

func (i *macOSInstance) platform() string {
	if i.runtime == "windows-app" {
		return "windows"
	}
	return "macos"
}

func (i *macOSInstance) emit(event sdk.EngineEvent) {
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

func (i *macOSInstance) finish(event sdk.EngineEvent) {
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
