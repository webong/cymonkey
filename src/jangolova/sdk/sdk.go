// Package sdk defines the public boundary between a Jangolova runtime module
// and its host. It contains no coordinator, policy store, credential resolver,
// or process implementation.
package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gorilla/websocket"
)

type Caller interface {
	Call(context.Context, string, json.RawMessage) (json.RawMessage, error)
}

// RemoteError preserves an engine's structured failure code for callers.
// Message is untrusted runtime text and must be redacted before host logging.
type RemoteError struct {
	Code    string
	Message string
}

func (e *RemoteError) Error() string { return "runtime " + e.Code + ": " + e.Message }

type Transport interface {
	Caller
	Close() error
}
type EngineSpec struct {
	Adapter              string          `json:"adapter"`
	RequiredCapabilities []string        `json:"requiredCapabilities,omitempty"`
	Source               string          `json:"source,omitempty"`
	Options              json.RawMessage `json:"options,omitempty"`
}
type EngineTarget struct {
	APIVersion string
	TargetID   string
	Kind       string
	Endpoints  []TargetEndpoint
	Handles    map[string]string
	Metadata   map[string]string
}

func (t EngineTarget) Endpoint(protocol string) (TargetEndpoint, bool) {
	for _, e := range t.Endpoints {
		if e.Protocol == protocol {
			return e, true
		}
	}
	return TargetEndpoint{}, false
}

// TargetEndpoint carries coordinates and a host-owned material view. Modules
// cannot mint, rotate, persist, or clear credentials through this interface.
type TargetEndpoint struct {
	Name, Protocol, URL, CredentialRef, TLSRef, Audience string
	Metadata                                             map[string]string
	Connection                                           ConnectionMaterial `json:"-"`
}
type ConnectionMaterial interface {
	Snapshot() EndpointConnectionSnapshot
	Updates() <-chan uint64
	Acknowledge(uint64)
}
type EndpointConnectionSnapshot struct {
	Headers                                   map[string]string
	Revision, CredentialRevision, TLSRevision uint64
}

func (e TargetEndpoint) Snapshot() EndpointConnectionSnapshot {
	if e.Connection == nil {
		return EndpointConnectionSnapshot{}
	}
	return e.Connection.Snapshot()
}

type AuthorizeRequest struct {
	TargetID     string          `json:"targetId"`
	Action       string          `json:"action"`
	Input        json.RawMessage `json:"input,omitempty"`
	Capabilities []string        `json:"capabilities,omitempty"`
}
type AuthorizeDecision struct {
	Authorized bool   `json:"authorized"`
	Reason     string `json:"reason,omitempty"`
}

// Authorize checks module support and host-supplied limits. A positive result
// does not replace the operator's approval decision.
type EngineInstance interface {
	Disconnect(context.Context) error
	Authorize(context.Context, AuthorizeRequest) (AuthorizeDecision, error)
}
type EngineAdapter interface {
	Connect(context.Context, EngineSpec, EngineTarget) (EngineInstance, error)
}
type EngineEvent struct {
	Type, Status, Message string
	OccurredAt            time.Time
}
type EngineEventSource interface{ EngineEvents() <-chan EngineEvent }
type EngineHealth struct {
	Status, Message string
	ObservedAt      time.Time
}
type EngineHealthProvider interface {
	EngineHealth(context.Context) EngineHealth
}
type EngineCapabilityProvider interface{ EngineCapabilities() []string }
type EngineInspection struct {
	Available    bool
	Capabilities []string
	Message      string
}
type EngineInspector interface {
	InspectEngine(context.Context) EngineInspection
}
type CallerLaunch struct{ Environment map[string]string }
type EngineCallerLaunchProvider interface{ EngineCallerLaunch() CallerLaunch }

const (
	EngineHealthStarting  = "connecting"
	EngineHealthStopping  = "disconnecting"
	EngineHealthHealthy   = "connected"
	EngineHealthUnhealthy = "unhealthy"
	EngineHealthStopped   = "disconnected"
	EngineHealthUnknown   = "unknown"
	MethodHello           = "hello"
	MethodCapabilities    = "capabilities"
	MethodDescribe        = "describe"
	MethodAct             = "act"
	MethodEvents          = "events"
	EffectRead            = "read"
	EffectWrite           = "write"
	EffectExternal        = "external"
)

// Capability is the minimal legacy bridge descriptor, used when mapping a
// bridge into the richer runtime contract in ../contract.
type Capability struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Effect      string          `json:"effect"`
}
type Worker interface {
	Caller
	Disconnect(context.Context) error
	Terminate()
	Done() <-chan struct{}
	WaitError() error
	StderrSuffix() string
}
type Listener interface {
	Endpoint() string
	Token() string
	WaitConnection(context.Context) (Transport, error)
	Close(context.Context) error
}
type WebSocketHostProvider interface{ BridgeWebSocketHost() Listener }

// Host supplies only the services a selected module needs. Omitted services
// fail explicitly; there is no implicit privileged host or global registry.
// Policy decisions remain with the calling operator.
type Host struct {
	ValidateEndpoint  func(TargetEndpoint) error
	WorkerEnvironment func(TargetEndpoint, []string) ([]string, error)
	StartWorker       func(string, string, []string, []string) (Worker, error)
	DialWebSocket     func(context.Context, TargetEndpoint) (*websocket.Conn, error)
	ListenWebSocket   func(string) (Listener, error)
	ConnectSafari     func(context.Context, EngineSpec, EngineTarget) (EngineInstance, error)
	Redact            func(string, EngineTarget) string
}

func (h Host) Validate(e TargetEndpoint) error {
	if h.ValidateEndpoint == nil {
		return errors.New("Jangolova host endpoint validation is required")
	}
	return h.ValidateEndpoint(e)
}
func (h Host) NodeEnvironment(e TargetEndpoint, env []string) ([]string, error) {
	if h.WorkerEnvironment == nil {
		return nil, errors.New("Jangolova host worker environment is required")
	}
	return h.WorkerEnvironment(e, env)
}
func (h Host) RedactString(message string, target EngineTarget) string {
	if h.Redact == nil {
		return "Jangolova host operation failed"
	}
	return h.Redact(message, target)
}
