package blockade

import (
	"context"
)

// Engine is Blockade's runtime boundary for a started inference provider.
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

// StartConfiguredInference starts the selected provider.
func StartConfiguredInference(ctx context.Context, config ProviderAdapterConfig, registry *ProviderAdapterRegistry, resolver SecretResolver) (Engine, error) {
	return StartConfiguredProviderAdapter(ctx, config, registry, resolver)
}
