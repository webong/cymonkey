// Package displaykit defines the display.engine contract: how a host connects
// a display engine to a caller-owned target when the engine runs as an ext
// package. The host resolves and owns credentials; it sends the prepared
// connection material with the target, and the engine uses it to reach the
// target. The types here are the wire shape both sides share.
package displaykit

import (
	"encoding/json"
	"time"
)

const (
	ContractName    = "display.engine"
	ContractVersion = "v1"
)

// Operations lists every method a display.engine package serves.
var Operations = []string{"inspect", "connect", "call", "authorize", "health", "disconnect", "events", "material"}

// Inspection reports whether the engine can run here and what it can do.
type Inspection struct {
	Available    bool     `json:"available"`
	Capabilities []string `json:"capabilities,omitempty"`
	Message      string   `json:"message,omitempty"`
}

// Spec selects and configures the engine for one connection.
type Spec struct {
	Adapter              string          `json:"adapter"`
	RequiredCapabilities []string        `json:"requiredCapabilities,omitempty"`
	Source               string          `json:"source,omitempty"`
	Options              json.RawMessage `json:"options,omitempty"`
}

// TLS names the files a connection uses for certificate trust. Paths are not
// secret; the files they name stay on the host's machine.
type TLS struct {
	CAFile                string `json:"caFile,omitempty"`
	ClientCertificateFile string `json:"clientCertificateFile,omitempty"`
	ClientKeyFile         string `json:"clientKeyFile,omitempty"`
	ServerName            string `json:"serverName,omitempty"`
}

// Connection is the prepared material for one endpoint at one revision. Headers
// and SecretValues are sensitive: Headers carry the credentials the engine
// presents to the target, and SecretValues lists strings the engine must remove
// from anything it reports.
type Connection struct {
	Headers            map[string]string `json:"headers,omitempty"`
	TLS                *TLS              `json:"tls,omitempty"`
	ExpiresAt          time.Time         `json:"expiresAt,omitempty"`
	Revision           uint64            `json:"revision,omitempty"`
	CredentialRevision uint64            `json:"credentialRevision,omitempty"`
	TLSRevision        uint64            `json:"tlsRevision,omitempty"`
	SecretValues       []string          `json:"secretValues,omitempty"`
}

// Endpoint is one coordinate of a target with its prepared material.
type Endpoint struct {
	Name          string            `json:"name"`
	Protocol      string            `json:"protocol"`
	URL           string            `json:"url"`
	CredentialRef string            `json:"credentialRef,omitempty"`
	TLSRef        string            `json:"tlsRef,omitempty"`
	Audience      string            `json:"audience,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Connection    *Connection       `json:"connection,omitempty"`
}

// Target is the complete caller-resolved description of what to attach to.
type Target struct {
	APIVersion string            `json:"apiVersion"`
	TargetID   string            `json:"targetId"`
	Kind       string            `json:"kind"`
	Endpoints  []Endpoint        `json:"endpoints"`
	Handles    map[string]string `json:"handles,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// ConnectRequest opens the engine's one session in this process.
type ConnectRequest struct {
	Spec   Spec   `json:"spec"`
	Target Target `json:"target"`
}

// ConnectResult describes the session that was opened. CallerLaunch holds
// ephemeral material for launching a helper that connects back; it must not be
// persisted or logged.
type ConnectResult struct {
	Capabilities []string     `json:"capabilities,omitempty"`
	CallerLaunch CallerLaunch `json:"callerLaunch"`
}

type CallerLaunch struct {
	Environment map[string]string `json:"environment,omitempty"`
}

// CallRequest invokes one semantic method of the engine's runtime.
type CallRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// AuthorizeRequest and AuthorizeDecision carry the engine's own policy check
// for an interaction.
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

// Health is a session health observation.
type Health struct {
	Status     string    `json:"status"`
	Message    string    `json:"message,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
}

// Event is a lifecycle or health transition reported by the session.
type Event struct {
	Type       string    `json:"type"`
	Status     string    `json:"status,omitempty"`
	Message    string    `json:"message,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

// EventsRequest asks for events after a cursor; an empty cursor starts at the
// oldest retained event.
type EventsRequest struct {
	Cursor string `json:"cursor,omitempty"`
}

type EventsResult struct {
	Cursor string  `json:"cursor"`
	Events []Event `json:"events"`
}

// MaterialUpdate replaces the prepared material for one endpoint of the
// connected target when the host renews it.
type MaterialUpdate struct {
	Endpoint   int        `json:"endpoint"`
	Connection Connection `json:"connection"`
}
