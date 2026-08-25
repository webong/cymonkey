package cymonkey

import (
	"context"
	"encoding/json"

	contract "jangolova/internal/cymonkey"
	"jangolova/internal/manifest"
	"jangolova/internal/orchestrator"
)

// Backend is the runtime-specific boundary below Cymonkey's stable semantic
// contract. Implementations attach to caller-owned targets and must not own the
// browser or application lifecycle.
type Backend interface {
	Name() BackendName
	Profile() contract.Profile
	Compatible(orchestrator.EngineTarget) bool
	Connect(context.Context, manifest.EngineSpec, orchestrator.EngineTarget, options) (orchestrator.EngineInstance, error)
}

type extensionMode string

const (
	extensionAuto     extensionMode = "auto"
	extensionDisabled extensionMode = "disabled"
	extensionRequired extensionMode = "required"
)

type extensionOptions struct {
	Mode extensionMode `json:"mode,omitempty"`
	ID   string        `json:"id,omitempty"`
}

type nativeOptions struct {
	ControlListen string `json:"controlListen,omitempty"`
}

type policyOptions struct {
	AllowedCapabilities []string `json:"allowedCapabilities,omitempty"`
	AllowedOrigins      []string `json:"allowedOrigins,omitempty"`
	AllowedBundleIDs    []string `json:"allowedBundleIds,omitempty"`
}

type options struct {
	Profile    contract.Profile `json:"profile,omitempty"`
	Backend    string           `json:"backend,omitempty"`
	Driver     string           `json:"driver,omitempty"`
	NodePath   string           `json:"nodePath,omitempty"`
	WorkerPath string           `json:"workerPath,omitempty"`
	Extension  extensionOptions `json:"extension,omitempty"`
	Native     nativeOptions    `json:"native,omitempty"`
	Policy     policyOptions    `json:"policy,omitempty"`

	// extensionId is accepted as a compatibility alias for the original
	// extension-only prototype.
	ExtensionID string `json:"extensionId,omitempty"`
}

type semanticAction struct {
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func decodeAction(raw json.RawMessage) (semanticAction, error) {
	var action semanticAction
	err := json.Unmarshal(raw, &action)
	if action.Input == nil {
		action.Input = map[string]any{}
	}
	return action, err
}
