package jangolova

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	contract "jangolova/contract"
	"jangolova/sdk"
)

type safariMCPBackend struct{}

func (safariMCPBackend) Name() BackendName { return BackendSafariMCP }
func (safariMCPBackend) Domains() []contract.Domain {
	return []contract.Domain{contract.DomainViewer, contract.DomainRender}
}
func (safariMCPBackend) Compatible(target sdk.EngineTarget) bool {
	_, ok := target.Endpoint("mcp-streamable-http")
	return target.Kind == "browser" && ok
}

func (safariMCPBackend) Connect(ctx context.Context, spec sdk.EngineSpec, target sdk.EngineTarget, config Options) (sdk.EngineInstance, error) {
	// Safari MCP owns its own transport options. Cymonkey policy and backend
	// selection options must not be passed through to that adapter.
	if config.Host.ConnectSafari == nil {
		return nil, errors.New("Jangolova host Safari connector is required")
	}
	underlying, err := config.Host.ConnectSafari(ctx, sdk.EngineSpec{Source: spec.Source}, target)
	if err != nil {
		return nil, err
	}
	caller, ok := underlying.(sdk.Caller)
	if !ok {
		_ = underlying.Disconnect(context.Background())
		return nil, errors.New("Safari MCP backend does not implement bridge calls")
	}
	raw, err := caller.Call(ctx, sdk.MethodCapabilities, json.RawMessage(`{}`))
	if err != nil {
		_ = underlying.Disconnect(context.Background())
		return nil, fmt.Errorf("discover Safari MCP Cymonkey mappings: %w", err)
	}
	var discovered []sdk.Capability
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
	underlying   sdk.EngineInstance
	caller       sdk.Caller
	mappings     map[string]safariMapping
	capabilities []Capability
	policy       PolicyLimits
}

func (instance *safariInstance) Disconnect(ctx context.Context) error {
	return instance.underlying.Disconnect(ctx)
}

func (instance *safariInstance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "health":
		health := instance.EngineHealth(ctx)
		return json.Marshal(map[string]any{"status": health.Status, "observedAt": health.ObservedAt})
	case sdk.MethodHello:
		return json.Marshal(Hello{
			ProtocolVersion: ProtocolVersion,
			Implementation:  Implementation{Name: "jangolova-cymonkey", Version: "0.1.0"},
			Domains:         []contract.Domain{contract.DomainViewer, contract.DomainRender},
			Runtimes:        []string{"browser-dom"},
			Drivers:         []BackendName{BackendSafariMCP},
			Features:        []string{"caller-owned-target", "capabilities.negotiated", "safari-mcp.dynamic-mapping"},
		})
	case sdk.MethodCapabilities:
		return json.Marshal(instance.capabilities)
	case sdk.MethodDescribe:
		description, err := instance.caller.Call(ctx, method, params)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"revision": "safari-mcp", "surfaces": []any{}, "augmentations": []any{}, "driver": BackendSafariMCP, "mappedCapabilities": capabilityNamesFromDescriptors(instance.capabilities), "target": json.RawMessage(description)})
	case sdk.MethodAct:
		return instance.act(ctx, params)
	case sdk.MethodEvents:
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
	return instance.caller.Call(ctx, sdk.MethodAct, payload)
}

func (instance *safariInstance) Authorize(ctx context.Context, request sdk.AuthorizeRequest) (sdk.AuthorizeDecision, error) {
	action := strings.TrimSpace(request.Action)
	if action == "" {
		return sdk.AuthorizeDecision{Authorized: false}, errors.New("Cymonkey Safari action name is required")
	}
	if !capabilityAllowed(instance.policy.AllowedCapabilities, action) {
		return sdk.AuthorizeDecision{Authorized: false, Reason: fmt.Sprintf("Cymonkey policy denied capability %q", action)}, nil
	}
	return instance.underlying.Authorize(ctx, request)
}

func (instance *safariInstance) EngineCapabilities() []string {
	return stableStrings(append([]string{"act", "capabilities", "describe", "events", "target.safari-mcp"}, capabilityNamesFromDescriptors(instance.capabilities)...))
}

func (instance *safariInstance) EngineHealth(ctx context.Context) sdk.EngineHealth {
	if provider, ok := instance.underlying.(sdk.EngineHealthProvider); ok {
		return provider.EngineHealth(ctx)
	}
	return sdk.EngineHealth{Status: sdk.EngineHealthHealthy}
}

func (instance *safariInstance) EngineEvents() <-chan sdk.EngineEvent {
	if source, ok := instance.underlying.(sdk.EngineEventSource); ok {
		return source.EngineEvents()
	}
	closed := make(chan sdk.EngineEvent)
	close(closed)
	return closed
}

func mapSafariCapabilities(discovered []sdk.Capability, allowed []string) (map[string]safariMapping, []Capability) {
	mappings := make(map[string]safariMapping)
	capabilities := make([]Capability, 0)
	add := func(name string, domain contract.Domain, mapping safariMapping, effect string, schema json.RawMessage) {
		if _, exists := mappings[name]; exists || !capabilityAllowed(allowed, name) {
			return
		}
		mappings[name] = mapping
		capabilities = append(capabilities, Capability{
			Name: name, Domain: domain, Runtime: "browser-dom", Driver: BackendSafariMCP, Support: SupportMapped,
			Lifetime: LifetimeCall, Persistence: PersistenceEphemeral,
			Effect: effect, InputSchema: schema,
		})
	}
	for _, capability := range discovered {
		lower := strings.ToLower(capability.Name)
		mapping := safariMapping{action: capability.Name}
		if capability.Name == "window.evaluate" {
			add("window.evaluate", contract.DomainViewer, mapping, "external", objectSchema("expression"))
			continue
		}
		if !strings.HasPrefix(lower, "mcp.tool.") {
			continue
		}
		tool := strings.TrimPrefix(capability.Name, "mcp.tool.")
		mapping = safariMapping{tool: tool}
		switch {
		case strings.Contains(lower, "preload") && containsAny(lower, "add", "register", "install"):
			add("script.register", contract.DomainRender, mapping, "external", capability.InputSchema)
		case strings.Contains(lower, "preload") && containsAny(lower, "remove", "unregister"):
			add("script.unregister", contract.DomainRender, mapping, "external", capability.InputSchema)
		case strings.Contains(lower, "network") && containsAny(lower, "observe", "event", "traffic", "request"):
			add("network.observe", contract.DomainViewer, mapping, "read", capability.InputSchema)
		case strings.Contains(lower, "network") && containsAny(lower, "add_intercept", "install_rule", "register_intercept"):
			add("network.rules.install", contract.DomainViewer, mapping, "external", capability.InputSchema)
		case strings.Contains(lower, "network") && containsAny(lower, "remove_intercept", "remove_rule", "unregister_intercept"):
			add("network.rules.remove", contract.DomainViewer, mapping, "external", capability.InputSchema)
		case containsAny(lower, "dom_query", "locate_node", "find_element"):
			add("document.query", contract.DomainRender, mapping, "read", capability.InputSchema)
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
var _ sdk.EngineInstance = (*safariInstance)(nil)
var _ sdk.EngineHealthProvider = (*safariInstance)(nil)
var _ sdk.EngineCapabilityProvider = (*safariInstance)(nil)
var _ sdk.EngineEventSource = (*safariInstance)(nil)
