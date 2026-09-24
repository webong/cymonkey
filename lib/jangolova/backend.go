package jangolova

import (
	"context"
	"encoding/json"

	contract "cymonkey/lib/jangolova/contract"
	"cymonkey/lib/jangolova/sdk"
)

// Backend is the runtime-specific boundary below Jangolova's public semantic
// contract. Implementations attach to caller-owned targets and must not own the
// browser or application lifecycle.
type Backend interface {
	Name() BackendName
	Domains() []contract.Domain
	Compatible(sdk.EngineTarget) bool
	Connect(context.Context, sdk.EngineSpec, sdk.EngineTarget, Options) (sdk.EngineInstance, error)
}

type extensionMode string

const (
	extensionAuto     extensionMode = "auto"
	extensionDisabled extensionMode = "disabled"
	extensionRequired extensionMode = "required"
)

type ExtensionOptions struct {
	Mode extensionMode `json:"mode,omitempty"`
	ID   string        `json:"id,omitempty"`
}

type NativeOptions struct {
	ControlListen string `json:"controlListen,omitempty"`
}

type PolicyLimits struct {
	AllowedCapabilities []string `json:"allowedCapabilities,omitempty"`
	AllowedOrigins      []string `json:"allowedOrigins,omitempty"`
	AllowedBundleIDs    []string `json:"allowedBundleIds,omitempty"`
}

// compositeOptions describes explicit caller-owned module bindings. One
// binding uses the Adapter target; every other binding supplies a second
// caller-owned target. A composite never discovers or launches either.
type compositeOptions struct {
	Bindings []compositeBindingOptions `json:"bindings"`
}

type compositeBindingOptions struct {
	ID     string           `json:"id"`
	Module string           `json:"module"`
	Target *compositeTarget `json:"target,omitempty"`
}

type compositeTarget struct {
	APIVersion string                    `json:"apiVersion,omitempty"`
	TargetID   string                    `json:"targetId,omitempty"`
	Kind       string                    `json:"kind"`
	Endpoints  []compositeTargetEndpoint `json:"endpoints,omitempty"`
	Metadata   map[string]string         `json:"metadata,omitempty"`
}

type compositeTargetEndpoint struct {
	Name          string            `json:"name,omitempty"`
	Protocol      string            `json:"protocol"`
	URL           string            `json:"url"`
	CredentialRef string            `json:"credentialRef,omitempty"`
	TLSRef        string            `json:"tlsRef,omitempty"`
	Audience      string            `json:"audience,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// Options contains module configuration and explicit host-supplied limits.
// Host is never decoded from runtime JSON.
type Options struct {
	Host       sdk.Host          `json:"-"`
	Domain     contract.Domain   `json:"domain,omitempty"`
	Module     string            `json:"module,omitempty"`
	Driver     string            `json:"driver,omitempty"`
	NodePath   string            `json:"nodePath,omitempty"`
	WorkerPath string            `json:"workerPath,omitempty"`
	Extension  ExtensionOptions  `json:"extension,omitempty"`
	Native     NativeOptions     `json:"native,omitempty"`
	Policy     PolicyLimits      `json:"policy,omitempty"`
	Composite  *compositeOptions `json:"composite,omitempty"`
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
