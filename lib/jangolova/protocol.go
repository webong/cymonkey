package cymonkey

import (
	"encoding/json"

	contract "cymonkey/internal/cymonkey"
)

const ProtocolVersion = contract.ProtocolVersion

type Domain = contract.Domain

const (
	DomainViewer = contract.DomainViewer
	DomainRender = contract.DomainRender
	DomainPlayer = contract.DomainPlayer
)

type BackendName string

const (
	BackendCDP                BackendName = "cdp"
	BackendBiDi               BackendName = "bidi"
	BackendSafariMCP          BackendName = "safari-mcp"
	BackendWebExtension       BackendName = "webextension"
	BackendMacOSAppleEvents   BackendName = "macos-apple-events"
	BackendMacOSAccessibility BackendName = "macos-accessibility"
	BackendMacOSCooperative   BackendName = "macos-cooperative"
	BackendWindowsCooperative BackendName = "windows-cooperative"
)

type SupportMode string

const (
	SupportNative   SupportMode = "native"
	SupportMapped   SupportMode = "mapped"
	SupportEmulated SupportMode = "emulated"
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

type Capability struct {
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	Domain        contract.Domain `json:"domain"`
	Runtime       string          `json:"runtime"`
	Driver        BackendName     `json:"driver"`
	Support       SupportMode     `json:"support"`
	Lifetime      Lifetime        `json:"lifetime"`
	Persistence   Persistence     `json:"persistence"`
	Effect        string          `json:"effect"`
	ResourceKinds []string        `json:"resourceKinds,omitempty"`
	InputSchema   json.RawMessage `json:"inputSchema"`
	Alternatives  []BackendName   `json:"alternatives,omitempty"`
}

type Hello struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Implementation  implementation    `json:"implementation"`
	Domains         []contract.Domain `json:"domains"`
	Runtimes        []string          `json:"runtimes"`
	Drivers         []BackendName     `json:"drivers"`
	Features        []string          `json:"features,omitempty"`
}

type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

func objectSchema(required ...string) json.RawMessage {
	value, _ := json.Marshal(map[string]any{"type": "object", "required": required, "additionalProperties": true})
	return value
}
