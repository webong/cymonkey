package cymonkey

import (
	"fmt"
	"sort"
	"strings"
)

// Registry is a host-controlled allowlist of compiled Cymonkey modules.
type Registry struct {
	modules map[string]Module
	order   []string
}

func NewRegistry(modules ...Module) (*Registry, error) {
	registry := &Registry{modules: map[string]Module{}}
	for _, module := range modules {
		if err := registry.Register(module); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (registry *Registry) Register(module Module) error {
	if module == nil {
		return fmt.Errorf("cannot register nil Cymonkey module")
	}
	descriptor := module.Descriptor()
	if err := ValidateModuleDescriptor(descriptor); err != nil {
		return err
	}
	if _, exists := registry.modules[descriptor.ID]; exists {
		return fmt.Errorf("Cymonkey module %q is already registered", descriptor.ID)
	}
	registry.modules[descriptor.ID] = module
	registry.order = append(registry.order, descriptor.ID)
	sort.Strings(registry.order)
	return nil
}

func (registry *Registry) Module(id string) (Module, bool) {
	module, ok := registry.modules[id]
	return module, ok
}

func (registry *Registry) Modules() []ModuleDescriptor {
	result := make([]ModuleDescriptor, 0, len(registry.order))
	for _, id := range registry.order {
		result = append(result, registry.modules[id].Descriptor())
	}
	return result
}

// Resolve selects an explicit module or the sole module matching domain,
// runtime, and optional driver. Ambiguous automatic choices are rejected.
func (registry *Registry) Resolve(moduleID string, domain Domain, runtime, driver string) (Module, error) {
	if moduleID != "" {
		module, ok := registry.Module(moduleID)
		if !ok {
			return nil, fmt.Errorf("unknown Cymonkey module %q", moduleID)
		}
		if !supports(module.Descriptor(), domain, runtime, driver) {
			return nil, fmt.Errorf("Cymonkey module %q does not support requested binding", moduleID)
		}
		return module, nil
	}
	var candidates []Module
	for _, id := range registry.order {
		module := registry.modules[id]
		if supports(module.Descriptor(), domain, runtime, driver) {
			candidates = append(candidates, module)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no Cymonkey module supports %s/%s with driver %s", domain, runtime, driver)
	}
	if len(candidates) > 1 {
		ids := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			ids = append(ids, candidate.Descriptor().ID)
		}
		return nil, fmt.Errorf("ambiguous Cymonkey module selection: %s", strings.Join(ids, ", "))
	}
	return candidates[0], nil
}

func supports(descriptor ModuleDescriptor, domain Domain, runtime, driver string) bool {
	matchedRuntime := false
	for _, binding := range descriptor.Runtimes {
		if binding.Domain == domain && (runtime == "" || binding.Runtime == runtime) {
			matchedRuntime = true
			break
		}
	}
	if !matchedRuntime {
		return false
	}
	if driver == "" {
		return true
	}
	for _, candidate := range descriptor.Drivers {
		if candidate.ID == driver {
			return true
		}
	}
	return false
}
