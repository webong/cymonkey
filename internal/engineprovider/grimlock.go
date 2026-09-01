package engineprovider

import (
	"encoding/json"
	"strings"

	grimlockapi "github.com/akive/grimoire/subsystems/grimlock/api/v1alpha1"
)

// GrimlockTools projects Jangolova engine capabilities into Grimlock's
// portable catalogue. Execution remains in Jangolova and is selected through
// the executor binding.
func GrimlockTools(engines []EngineDescriptor) []grimlockapi.Tool {
	tools := make([]grimlockapi.Tool, 0)
	for _, engine := range engines {
		if !engine.Available {
			continue
		}
		for _, capability := range stableCapabilities(engine.Capabilities) {
			name := strings.Join([]string{"jangolova", engine.Adapter, capability}, ".")
			tools = append(tools, grimlockapi.Tool{
				APIVersion: grimlockapi.APIVersion,
				Kind:       grimlockapi.KindTool,
				Metadata:   grimlockapi.Metadata{Name: name, Version: "1.0.0"},
				Spec: grimlockapi.ToolSpec{
					Description:  "Invoke Jangolova capability " + capability + " through the " + engine.Adapter + " engine.",
					Effect:       grimlockEffect(capability),
					InputSchema:  json.RawMessage(`{"type":"object"}`),
					OutputSchema: json.RawMessage(`{"type":"object"}`),
					Idempotency:  grimlockapi.IdempotencyCallerKeyRequired,
					Executor: grimlockapi.ExecutorBinding{
						Kind:       "jangolova-engine",
						Binding:    engine.Adapter,
						Capability: capability,
					},
				},
			})
		}
	}
	return tools
}

func grimlockEffect(capability string) grimlockapi.Effect {
	capability = strings.ToLower(capability)
	for _, readMarker := range []string{"read", "list", "get", "inspect", "observe", "handles", "status"} {
		if strings.Contains(capability, readMarker) {
			return grimlockapi.EffectRead
		}
	}
	if strings.Contains(capability, "connect") || strings.Contains(capability, "network") {
		return grimlockapi.EffectExternal
	}
	return grimlockapi.EffectWrite
}
