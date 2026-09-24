package blockade

import (
	"context"
	"fmt"
)

// Engine is the common Blockade inference boundary. Local engines implement it
// directly; hosted providers are wrapped by the provider-adapter boundary.
type Engine interface {
	Observe(context.Context, ObserveRequest) (ObserveResponse, error)
	Close() error
}

type EngineCapabilities interface {
	Capabilities(context.Context) ([]string, error)
}

type EngineHealth struct {
	Ready  bool
	Detail string
}

type EngineHealthReporter interface {
	Health(context.Context) (EngineHealth, error)
}

// StartConfiguredEngine starts the engine matching the configured kind.
// Local Ultralytics engines run managed subprocess workers; native engines
// such as ONNX Runtime run in-process through cgo.
func StartConfiguredEngine(ctx context.Context, e EngineConfig) (Engine, error) {
	switch e.Kind {
	case "local-ultralytics":
		return StartConfiguredLocalEngine(ctx, e)
	case "onnx":
		return StartOnnxEngine(e)
	default:
		return nil, fmt.Errorf("engine %q has unsupported kind %q", e.ID, e.Kind)
	}
}

// StartConfiguredInference starts either a local engine or a Blockade-owned
// provider adapter selected from the same configuration namespace.
func StartConfiguredInference(ctx context.Context, config InferenceConfig, registry *ProviderAdapterRegistry, resolver SecretResolver) (Engine, error) {
	if config.Engine != nil && config.ProviderAdapter != nil {
		return nil, fmt.Errorf("Blockade inference %q selects both an engine and provider adapter", config.ID())
	}
	if config.Engine != nil {
		return StartConfiguredEngine(ctx, *config.Engine)
	}
	if config.ProviderAdapter != nil {
		return StartConfiguredProviderAdapter(ctx, *config.ProviderAdapter, registry, resolver)
	}
	return nil, fmt.Errorf("Blockade inference selection is empty")
}
