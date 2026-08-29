package cymonkey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"jangolova/adapters/safarimcp"
	"jangolova/internal/bridge"
	contract "jangolova/internal/cymonkey"
	"jangolova/internal/manifest"
	"jangolova/internal/orchestrator"
)

type safariMCPBackend struct{}

func (safariMCPBackend) Name() BackendName       { return BackendSafariMCP }
func (safariMCPBackend) Domain() contract.Domain { return contract.DomainComputer }
func (safariMCPBackend) Compatible(target orchestrator.EngineTarget) bool {
	_, ok := target.Endpoint("mcp-streamable-http")
	return target.Kind == "browser" && ok
}

func (safariMCPBackend) Connect(ctx context.Context, spec manifest.EngineSpec, target orchestrator.EngineTarget, config options) (orchestrator.EngineInstance, error) {
	// Safari MCP owns its own transport options. Cymonkey policy and backend
	// selection options must not be passed through to that adapter.
	underlying, err := (safarimcp.Adapter{}).Connect(ctx, manifest.EngineSpec{Source: spec.Source}, target)
	if err != nil {
		return nil, err
	}
	caller, ok := underlying.(bridge.Caller)
	if !ok {
		_ = underlying.Disconnect(context.Background())
		return nil, errors.New("Safari MCP backend does not implement bridge calls")
	}
	raw, err := caller.Call(ctx, bridge.MethodCapabilities, json.RawMessage(`{}`))
	if err != nil {
		_ = underlying.Disconnect(context.Background())
		return nil, fmt.Errorf("discover Safari MCP Cymonkey mappings: %w", err)
	}
	var discovered []bridge.Capability
	if err := json.Unmarshal(raw, &discovered); err != nil {
		_ = underlying.Disconnect(context.Background())
		return nil, fmt.Errorf("decode Safari MCP capabilities: %w", err)
	}
	mappings, capabilities := mapSafariCapabilities(discovered, config.Policy.AllowedCapabilities)
	if missing := missingCapabilities(spec.RequiredCapabilities, capabilityNamesFromDescriptors(capabilities)); len(missing) != 0 {
		_ = underlying.Disconnect(context.Background())
		return nil, fmt.Errorf("Cymonkey Safari MCP mapping is missing required capabilities: %s", strings.Join(missing, ", "))
	}
	return &safariInstance{underlying: underlying, caller: caller, mappings: mappings, capabilities: capabilities, policy: config.Policy}, nil
}

type safariMapping struct {
	action string
	tool   string
}

type safariInstance struct {
	underlying   orchestrator.EngineInstance
	caller       bridge.Caller
	mappings     map[string]safariMapping
	capabilities []Capability
	policy       policyOptions
}

func (instance *safariInstance) Disconnect(ctx context.Context) error {
	return instance.underlying.Disconnect(ctx)
}

func (instance *safariInstance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case bridge.MethodHello:
		return json.Marshal(Hello{
			ProtocolVersion: ProtocolVersion,
			Implementation:  implementation{Name: "jangolova-cymonkey", Version: "0.1.0"},
			Domains:         []contract.Domain{contract.DomainComputer},
			Runtimes:        []string{"browser-dom"},
			Drivers:         []BackendName{BackendSafariMCP},
			Features:        []string{"caller-owned-target", "capabilities.negotiated", "safari-mcp.dynamic-mapping"},
		})
	case bridge.MethodCapabilities:
		return json.Marshal(instance.capabilities)
	case bridge.MethodDescribe:
		description, err := instance.caller.Call(ctx, method, params)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"revision": "safari-mcp", "surfaces": []any{}, "augmentations": []any{}, "driver": BackendSafariMCP, "mappedCapabilities": capabilityNamesFromDescriptors(instance.capabilities), "target": json.RawMessage(description)})
	case bridge.MethodAct:
		return instance.act(ctx, params)
	case bridge.MethodEvents:
		return instance.caller.Call(ctx, method, params)
	default:
		return nil, fmt.Errorf("unsupported Cymonkey method %q", method)
	}
}

func (instance *safariInstance) act(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	action, err := decodeAction(raw)
	if err != nil {
		return nil, fmt.Errorf("decode Cymonkey action: %w", err)
	}
	mapping, ok := instance.mappings[action.Name]
	if !ok || !capabilityAllowed(instance.policy.AllowedCapabilities, action.Name) {
		return nil, fmt.Errorf("Safari MCP does not advertise Cymonkey capability %q", action.Name)
	}
	if rawURL, _ := action.Input["url"].(string); !originAllowed(instance.policy.AllowedOrigins, rawURL) {
		return nil, fmt.Errorf("Cymonkey policy denied origin %q", rawURL)
	}
	input := action.Input
	if action.Name == "script.execute" {
		source, _ := input["source"].(string)
		if source == "" {
			source, _ = input["expression"].(string)
		}
		if source == "" {
			return nil, errors.New("script.execute input.source is required")
		}
		input = map[string]any{"expression": source}
	}
	forward := map[string]any{"name": mapping.action, "input": input}
	if mapping.tool != "" {
		forward["name"] = "mcp.tool." + mapping.tool
	}
	payload, _ := json.Marshal(forward)
	return instance.caller.Call(ctx, bridge.MethodAct, payload)
}

func (instance *safariInstance) Authorize(ctx context.Context, request orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	action := strings.TrimSpace(request.Action)
	if action == "" {
		return orchestrator.AuthorizeDecision{Authorized: false}, errors.New("Cymonkey Safari action name is required")
	}
	if !capabilityAllowed(instance.policy.AllowedCapabilities, action) {
		return orchestrator.AuthorizeDecision{Authorized: false, Reason: fmt.Sprintf("Cymonkey policy denied capability %q", action)}, nil
	}
	return instance.underlying.Authorize(ctx, request)
}

func (instance *safariInstance) EngineCapabilities() []string {
	return stableStrings(append([]string{"act", "capabilities", "describe", "events", "target.safari-mcp"}, capabilityNamesFromDescriptors(instance.capabilities)...))
}

func (instance *safariInstance) EngineHealth(ctx context.Context) orchestrator.EngineHealth {
	if provider, ok := instance.underlying.(orchestrator.EngineHealthProvider); ok {
		return provider.EngineHealth(ctx)
	}
	return orchestrator.EngineHealth{Status: orchestrator.EngineHealthHealthy}
}

func (instance *safariInstance) EngineEvents() <-chan orchestrator.EngineEvent {
	if source, ok := instance.underlying.(orchestrator.EngineEventSource); ok {
		return source.EngineEvents()
	}
	closed := make(chan orchestrator.EngineEvent)
	close(closed)
	return closed
}

func mapSafariCapabilities(discovered []bridge.Capability, allowed []string) (map[string]safariMapping, []Capability) {
	mappings := make(map[string]safariMapping)
	capabilities := make([]Capability, 0)
	add := func(name string, mapping safariMapping, effect string, schema json.RawMessage) {
		if _, exists := mappings[name]; exists || !capabilityAllowed(allowed, name) {
			return
		}
		mappings[name] = mapping
		capabilities = append(capabilities, Capability{
			Name: name, Domain: contract.DomainComputer, Runtime: "browser-dom", Driver: BackendSafariMCP, Support: SupportMapped,
			Lifetime: LifetimeCall, Persistence: PersistenceEphemeral,
			Effect: effect, InputSchema: schema,
		})
	}
	for _, capability := range discovered {
		lower := strings.ToLower(capability.Name)
		mapping := safariMapping{action: capability.Name}
		if capability.Name == "browser.evaluate" {
			add("script.execute", mapping, "external", objectSchema("source"))
			continue
		}
		if !strings.HasPrefix(lower, "mcp.tool.") {
			continue
		}
		tool := strings.TrimPrefix(capability.Name, "mcp.tool.")
		mapping = safariMapping{tool: tool}
		switch {
		case strings.Contains(lower, "preload") && containsAny(lower, "add", "register", "install"):
			add("script.register", mapping, "external", capability.InputSchema)
		case strings.Contains(lower, "preload") && containsAny(lower, "remove", "unregister"):
			add("script.unregister", mapping, "external", capability.InputSchema)
		case strings.Contains(lower, "network") && containsAny(lower, "observe", "event", "traffic", "request"):
			add("network.observe", mapping, "read", capability.InputSchema)
		case strings.Contains(lower, "network") && containsAny(lower, "add_intercept", "install_rule", "register_intercept"):
			add("network.rules.install", mapping, "external", capability.InputSchema)
		case strings.Contains(lower, "network") && containsAny(lower, "remove_intercept", "remove_rule", "unregister_intercept"):
			add("network.rules.remove", mapping, "external", capability.InputSchema)
		case containsAny(lower, "dom_query", "locate_node", "find_element"):
			add("dom.query", mapping, "read", capability.InputSchema)
		}
	}
	sort.Slice(capabilities, func(left, right int) bool { return capabilities[left].Name < capabilities[right].Name })
	return mappings, capabilities
}

func capabilityNamesFromDescriptors(values []Capability) []string {
	names := make([]string, 0, len(values))
	for _, value := range values {
		names = append(names, value.Name)
	}
	return names
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

var _ Backend = safariMCPBackend{}
var _ orchestrator.EngineInstance = (*safariInstance)(nil)
var _ orchestrator.EngineHealthProvider = (*safariInstance)(nil)
var _ orchestrator.EngineCapabilityProvider = (*safariInstance)(nil)
var _ orchestrator.EngineEventSource = (*safariInstance)(nil)
