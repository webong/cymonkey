// Code generated from protocol/cymonkey/v1alpha2/protocol.schema.json; DO NOT EDIT.
// Schema SHA-256: c7cbcf984edafe827da29e7c05d0a312b2895d8f54c26c5ef701c9a4944e4af0

package cymonkeyprotocol

import (
	"context"
	"encoding/json"
	"time"
)

const ProtocolVersion = "cymonkey/v1alpha1"

type DomainName string

const (
	DomainViewer DomainName = "viewer"
	DomainRender   DomainName = "render"
	DomainPlayer   DomainName = "player"
)

type DriverName string

const (
	DriverCDP                DriverName = "cdp"
	DriverBiDi               DriverName = "bidi"
	DriverSafariMCP          DriverName = "safari-mcp"
	DriverWebExtension       DriverName = "webextension"
	DriverMacOSAppleEvents   DriverName = "macos-apple-events"
	DriverMacOSAccessibility DriverName = "macos-accessibility"
	DriverMacOSCooperative   DriverName = "macos-cooperative"
	DriverWebSocket          DriverName = "websocket"
	DriverInPageRuntime      DriverName = "in-page-runtime"
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

type Effect string

const (
	EffectRead     Effect = "read"
	EffectWrite    Effect = "write"
	EffectExternal Effect = "external"
)

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type Hello struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Implementation  Implementation `json:"implementation"`
	Domains         []DomainName   `json:"domains"`
	Runtimes        []string       `json:"runtimes"`
	Drivers         []DriverName   `json:"drivers"`
	Features        []string       `json:"features,omitempty"`
}

type Capability struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Domain       DomainName      `json:"domain"`
	Runtime      string          `json:"runtime"`
	Driver       DriverName      `json:"driver"`
	Support      SupportMode     `json:"support"`
	Lifetime     Lifetime        `json:"lifetime"`
	Persistence  Persistence     `json:"persistence"`
	Effect       Effect          `json:"effect"`
	ResourceKinds []string       `json:"resourceKinds,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	Alternatives []DriverName    `json:"alternatives,omitempty"`
}

type Surface struct {
	ID         string         `json:"id"`
	Domain     DomainName     `json:"domain"`
	Runtime    string         `json:"runtime"`
	Kind       string         `json:"kind"`
	Label      string         `json:"label,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

type Augmentation struct {
	ID        string        `json:"id"`
	Revision  string        `json:"revision"`
	Enabled   bool          `json:"enabled"`
	Domains   []DomainName  `json:"domains,omitempty"`
}

type Description struct {
	Revision      string        `json:"revision"`
	Surfaces      []Surface     `json:"surfaces"`
	Augmentations []Augmentation `json:"augmentations"`
}

type Action struct {
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Domain  DomainName      `json:"domain,omitempty"`
	Runtime string          `json:"runtime,omitempty"`
}

type Event struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"`
	OccurredAt time.Time     `json:"occurredAt"`
	Domain     DomainName    `json:"domain"`
	Runtime    string        `json:"runtime"`
	Driver     DriverName    `json:"driver"`
	SurfaceID  string        `json:"surfaceId,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type EventBatch struct {
	Cursor string `json:"cursor"`
	Events []Event `json:"events"`
}

type CallRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type CallResponse struct {
	ProtocolVersion string          `json:"protocolVersion"`
	InstanceID      string          `json:"instanceId,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
}

type Transport interface {
	Call(context.Context, CallRequest) (CallResponse, error)
}

type Client struct {
	Transport Transport
}

func (c Client) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	response, err := c.Transport.Call(ctx, CallRequest{Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if len(response.Result) == 0 {
		return json.RawMessage("null"), nil
	}
	return response.Result, nil
}
