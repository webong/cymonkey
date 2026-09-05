package cymonkey

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

type Binding struct {
	ID     string
	Caller Caller
	Policy Policy
}

// Composite combines independently attached callers. It routes an action by
// the domain/runtime advertised by capabilities; ambiguous actions require an
// explicit selector.
type Composite struct {
	bindings []Binding
}

func NewComposite(bindings ...Binding) (*Composite, error) {
	seen := map[string]struct{}{}
	for _, binding := range bindings {
		if !ValidIdentifier(binding.ID) || binding.Caller == nil {
			return nil, fmt.Errorf("invalid Cymonkey composite binding")
		}
		if _, exists := seen[binding.ID]; exists {
			return nil, fmt.Errorf("duplicate Cymonkey composite binding %q", binding.ID)
		}
		seen[binding.ID] = struct{}{}
	}
	return &Composite{bindings: append([]Binding(nil), bindings...)}, nil
}

func (composite *Composite) Call(ctx context.Context, method string, raw json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "hello":
		return composite.hello(ctx)
	case "capabilities":
		return composite.capabilities(ctx)
	case "describe", "events":
		return composite.collect(ctx, method, raw)
	case "act":
		return composite.act(ctx, raw)
	default:
		return nil, fmt.Errorf("unsupported Cymonkey interaction method %q", method)
	}
}

func (composite *Composite) hello(ctx context.Context) (json.RawMessage, error) {
	var domains []Domain
	var runtimes, drivers []string
	for _, binding := range composite.bindings {
		raw, err := binding.Caller.Call(ctx, "hello", json.RawMessage(`{}`))
		if err != nil {
			return nil, fmt.Errorf("binding %q hello: %w", binding.ID, err)
		}
		var hello Hello
		if err := json.Unmarshal(raw, &hello); err != nil {
			return nil, fmt.Errorf("binding %q decode hello: %w", binding.ID, err)
		}
		if hello.ProtocolVersion != ProtocolVersion {
			return nil, fmt.Errorf("binding %q protocol version %q", binding.ID, hello.ProtocolVersion)
		}
		domains = append(domains, hello.Domains...)
		runtimes = append(runtimes, hello.Runtimes...)
		drivers = append(drivers, hello.Drivers...)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i] < domains[j] })
	dedupedDomains := make([]Domain, 0, len(domains))
	for _, domain := range domains {
		if len(dedupedDomains) == 0 || dedupedDomains[len(dedupedDomains)-1] != domain {
			dedupedDomains = append(dedupedDomains, domain)
		}
	}
	return json.Marshal(Hello{ProtocolVersion: ProtocolVersion, Domains: dedupedDomains, Runtimes: sortedStrings(runtimes), Drivers: sortedStrings(drivers)})
}

func (composite *Composite) capabilities(ctx context.Context) (json.RawMessage, error) {
	var result []Capability
	for _, binding := range composite.bindings {
		raw, err := binding.Caller.Call(ctx, "capabilities", json.RawMessage(`{}`))
		if err != nil {
			return nil, fmt.Errorf("binding %q capabilities: %w", binding.ID, err)
		}
		var capabilities []Capability
		if err := json.Unmarshal(raw, &capabilities); err != nil {
			return nil, fmt.Errorf("binding %q decode capabilities: %w", binding.ID, err)
		}
		for _, capability := range capabilities {
			if allowed(binding.Policy, capability.Name) {
				result = append(result, capability)
			}
		}
	}
	return json.Marshal(result)
}

func (composite *Composite) collect(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	result := make(map[string]json.RawMessage, len(composite.bindings))
	for _, binding := range composite.bindings {
		raw, err := binding.Caller.Call(ctx, method, params)
		if err != nil {
			return nil, fmt.Errorf("binding %q %s: %w", binding.ID, method, err)
		}
		result[binding.ID] = raw
	}
	return json.Marshal(result)
}

func (composite *Composite) act(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var action struct {
		Name    string          `json:"name"`
		Domain  Domain          `json:"domain,omitempty"`
		Runtime string          `json:"runtime,omitempty"`
		Binding string          `json:"binding,omitempty"`
		Input   json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &action); err != nil || action.Name == "" {
		return nil, fmt.Errorf("invalid Cymonkey action")
	}
	type candidate struct{ binding Binding }
	var candidates []candidate
	for _, binding := range composite.bindings {
		if action.Binding != "" && action.Binding != binding.ID {
			continue
		}
		capabilitiesRaw, err := binding.Caller.Call(ctx, "capabilities", json.RawMessage(`{}`))
		if err != nil {
			return nil, err
		}
		var capabilities []Capability
		if err := json.Unmarshal(capabilitiesRaw, &capabilities); err != nil {
			return nil, err
		}
		for _, capability := range capabilities {
			if capability.Name == action.Name && allowed(binding.Policy, capability.Name) &&
				(action.Domain == "" || action.Domain == capability.Domain) &&
				(action.Runtime == "" || action.Runtime == capability.Runtime) {
				candidates = append(candidates, candidate{binding})
				break
			}
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no composite binding advertises action %q", action.Name)
	}
	if len(candidates) > 1 {
		return nil, fmt.Errorf("action %q is ambiguous; provide binding, domain, or runtime", action.Name)
	}
	return candidates[0].binding.Caller.Call(ctx, "act", raw)
}

func allowed(policy Policy, capability string) bool {
	if len(policy.AllowedCapabilities) == 0 {
		return true
	}
	for _, allowed := range policy.AllowedCapabilities {
		if allowed == capability {
			return true
		}
	}
	return false
}
