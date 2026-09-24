package engineprovider

import (
	"context"
	"sort"
	"time"

	"cymonkey/src/internal/orchestrator"
)

const inspectionTimeout = 2 * time.Second

func DiscoverEngines(
	ctx context.Context,
	registry *orchestrator.Registry,
) []EngineDescriptor {
	names := registry.EngineNames()
	engines := make([]EngineDescriptor, 0, len(names))
	for _, name := range names {
		adapter, _ := registry.Engine(name)
		inspection := orchestrator.EngineInspection{
			Available: true,
			Capabilities: []string{
				"connect",
				"disconnect",
				"target.handles",
			},
		}
		if inspector, ok := adapter.(orchestrator.EngineInspector); ok {
			inspectionCtx, cancel := context.WithTimeout(ctx, inspectionTimeout)
			inspection = inspector.InspectEngine(inspectionCtx)
			cancel()
		}
		engines = append(engines, EngineDescriptor{
			Adapter:      name,
			Available:    inspection.Available,
			Capabilities: stableCapabilities(inspection.Capabilities),
			Message:      inspection.Message,
		})
	}
	return engines
}

func stableCapabilities(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	capabilities := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		capabilities = append(capabilities, value)
	}
	sort.Strings(capabilities)
	return capabilities
}
