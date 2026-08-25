package blockade

import (
	"context"
	"fmt"
)

// Engine is the Blockade-owned inference boundary. Implementations map
// provider-native models onto normalized ObserveResponses without exposing
// engine internals to callers.
type Engine interface {
	Observe(context.Context, ObserveRequest) (ObserveResponse, error)
	Close() error
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
