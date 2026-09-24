package orchestrator

import (
	"fmt"
	"sort"
	"strings"
)

type Registry struct {
	engines map[string]EngineAdapter
}

func NewRegistry() *Registry {
	return &Registry{engines: make(map[string]EngineAdapter)}
}

// Engine returns one registered interaction-engine adapter.
func (r *Registry) Engine(name string) (EngineAdapter, bool) {
	if r == nil {
		return nil, false
	}
	value, ok := r.engines[name]
	return value, ok
}

// EngineNames returns registered engine adapter names in stable order.
func (r *Registry) EngineNames() []string {
	if r == nil {
		return []string{}
	}
	names := make([]string, 0, len(r.engines))
	for name := range r.engines {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Registry) RegisterEngine(name string, adapter EngineAdapter) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("engine adapter name is required")
	}
	if adapter == nil {
		return fmt.Errorf("engine adapter %q is nil", name)
	}
	if _, exists := r.engines[name]; exists {
		return fmt.Errorf("engine adapter %q is already registered", name)
	}
	r.engines[name] = adapter
	return nil
}
