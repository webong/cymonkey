// Package cymonkey defines Jangolova's runtime-agnostic interaction contract.
// Runtime adapters map viewer, render, and player domain semantics to
// caller-owned targets.
package cymonkey

import "encoding/json"

const (
	ProtocolVersion  = "cymonkey/v1alpha1"
	AugmentationKind = "Augmentation"
)

type Domain string

const (
	DomainViewer Domain = "viewer"
	DomainRender Domain = "render"
	DomainPlayer Domain = "player"
)

type Driver string

const (
	DriverCDP                Driver = "cdp"
	DriverBiDi               Driver = "bidi"
	DriverSafariMCP          Driver = "safari-mcp"
	DriverWebExtension       Driver = "webextension"
	DriverMacOSAppleEvents   Driver = "macos-apple-events"
	DriverMacOSAccessibility Driver = "macos-accessibility"
	DriverMacOSCooperative   Driver = "macos-cooperative"
	DriverWebSocket          Driver = "websocket"
	DriverInPageRuntime      Driver = "in-page-runtime"
)

type Support string

const (
	SupportNative   Support = "native"
	SupportMapped   Support = "mapped"
	SupportEmulated Support = "emulated"
)

type Lifetime string

const (
	LifetimeCall         Lifetime = "call"
	LifetimeSurface      Lifetime = "surface"
	LifetimeAttachment   Lifetime = "attachment"
	LifetimeInstallation Lifetime = "installation"
)

type Persistence string

const (
	PersistenceEphemeral  Persistence = "ephemeral"
	PersistenceSession    Persistence = "session"
	PersistencePersistent Persistence = "persistent"
)

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type Hello struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Implementation  Implementation `json:"implementation"`
	Domains         []Domain       `json:"domains"`
	Runtimes        []string       `json:"runtimes"`
	Drivers         []Driver       `json:"drivers"`
	Features        []string       `json:"features,omitempty"`
}

type Capability struct {
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	Domain        Domain          `json:"domain"`
	Runtime       string          `json:"runtime"`
	Driver        Driver          `json:"driver"`
	Support       Support         `json:"support"`
	Lifetime      Lifetime        `json:"lifetime"`
	Persistence   Persistence     `json:"persistence"`
	Effect        string          `json:"effect"`
	ResourceKinds []string        `json:"resourceKinds,omitempty"`
	InputSchema   json.RawMessage `json:"inputSchema"`
	Alternatives  []Driver        `json:"alternatives,omitempty"`
}

type Surface struct {
	ID         string          `json:"id"`
	Domain     Domain          `json:"domain"`
	Runtime    string          `json:"runtime"`
	Kind       string          `json:"kind"`
	Label      string          `json:"label,omitempty"`
	Properties json.RawMessage `json:"properties,omitempty"`
}

type AugmentationSummary struct {
	ID       string   `json:"id"`
	Revision string   `json:"revision"`
	Enabled  bool     `json:"enabled"`
	Domains  []Domain `json:"domains,omitempty"`
}

type Description struct {
	Revision      string                `json:"revision"`
	Surfaces      []Surface             `json:"surfaces"`
	Augmentations []AugmentationSummary `json:"augmentations"`
}

type Manifest struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   ManifestMetadata `json:"metadata"`
	Spec       ManifestSpec     `json:"spec"`
}

type ManifestMetadata struct {
	ID       string            `json:"id"`
	Revision string            `json:"revision"`
	Labels   map[string]string `json:"labels,omitempty"`
}

type ManifestSpec struct {
	Targets     []Target        `json:"targets"`
	Permissions []string        `json:"permissions"`
	Enabled     *bool           `json:"enabled,omitempty"`
	Viewer      json.RawMessage `json:"viewer,omitempty"`
	Render      json.RawMessage `json:"render,omitempty"`
	Player      json.RawMessage `json:"player,omitempty"`
	Overlays    json.RawMessage `json:"overlays,omitempty"`
}

type Target struct {
	Domain  Domain          `json:"domain"`
	Runtime string          `json:"runtime"`
	Match   json.RawMessage `json:"match"`
}

// Action selects one advertised capability. Domain and Runtime are required
// only when a composite attachment has more than one binding for the name.
type Action struct {
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Domain  Domain          `json:"domain,omitempty"`
	Runtime string          `json:"runtime,omitempty"`
}
