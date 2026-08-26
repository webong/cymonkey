// Package cymonkey defines Jangolova's runtime-agnostic augmentation contract.
// Runtime adapters map these semantics to web, macOS, and future profiles.
package cymonkey

import "encoding/json"

const (
	ProtocolVersion   = "jangolova.cymonkey/v1alpha2"
	LegacyWebProtocol = "jangolova.cymonkey/v1alpha1"
	AugmentationKind  = "Augmentation"
)

type Profile string

const (
	ProfileWeb    Profile = "web"
	ProfileMacOS  Profile = "macos"
	ProfileEngine Profile = "engine"
)

type Backend string

const (
	BackendCDP                Backend = "cdp"
	BackendBiDi               Backend = "bidi"
	BackendSafariMCP          Backend = "safari-mcp"
	BackendWebExtension       Backend = "webextension"
	BackendMacOSAppleEvents   Backend = "macos-apple-events"
	BackendMacOSAccessibility Backend = "macos-accessibility"
	BackendMacOSCooperative   Backend = "macos-cooperative"
	BackendEngineGodot        Backend = "engine-godot"
	BackendEngineUnity        Backend = "engine-unity"
	BackendEngineUnreal       Backend = "engine-unreal"
	BackendEngineThreejs      Backend = "engine-threejs"
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
	ProtocolVersion     string         `json:"protocolVersion"`
	CompatibleProtocols []string       `json:"compatibleProtocols,omitempty"`
	Implementation      Implementation `json:"implementation"`
	Profiles            []Profile      `json:"profiles"`
	Backends            []Backend      `json:"backends"`
	Features            []string       `json:"features,omitempty"`
}

type Capability struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Profile      Profile         `json:"profile"`
	Backend      Backend         `json:"backend"`
	Support      Support         `json:"support"`
	Lifetime     Lifetime        `json:"lifetime"`
	Persistence  Persistence     `json:"persistence"`
	Effect       string          `json:"effect"`
	TargetKinds  []string        `json:"targetKinds,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	Alternatives []Backend       `json:"alternatives,omitempty"`
}

type Surface struct {
	ID         string          `json:"id"`
	Profile    Profile         `json:"profile"`
	Kind       string          `json:"kind"`
	Label      string          `json:"label,omitempty"`
	Properties json.RawMessage `json:"properties,omitempty"`
}

type AugmentationSummary struct {
	ID       string    `json:"id"`
	Revision string    `json:"revision"`
	Enabled  bool      `json:"enabled"`
	Profiles []Profile `json:"profiles,omitempty"`
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
	Web         json.RawMessage `json:"web,omitempty"`
	MacOS       json.RawMessage `json:"macos,omitempty"`
	Overlays    json.RawMessage `json:"overlays,omitempty"`
}

type Target struct {
	Profile Profile         `json:"profile"`
	Match   json.RawMessage `json:"match"`
}
