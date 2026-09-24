package orchestrator

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"cymonkey/src/internal/manifest"
)

// EngineInstance is a Jangolova-owned interaction session attached to a
// caller-owned target. Disconnect must release only Jangolova resources; it
// must never terminate the target runtime.
type EngineInstance interface {
	Disconnect(context.Context) error
	Authorize(context.Context, AuthorizeRequest) (AuthorizeDecision, error)
}

// AuthorizeRequest carries the policy-relevant fields for an interaction call.
type AuthorizeRequest struct {
	TargetID     string          `json:"targetId"`
	Action       string          `json:"action"`
	Input        json.RawMessage `json:"input,omitempty"`
	Capabilities []string        `json:"capabilities,omitempty"`
}

// AuthorizeDecision is the policy outcome for an Authorize call.
type AuthorizeDecision struct {
	Authorized bool   `json:"authorized"`
	Reason     string `json:"reason,omitempty"`
}

// CallerLaunch contains ephemeral material that an authorized target owner
// needs to launch a caller-owned cooperative helper. It is returned only from
// the initial connection response and must not be persisted or logged.
type CallerLaunch struct {
	Environment map[string]string
}

type EngineCallerLaunchProvider interface {
	EngineCallerLaunch() CallerLaunch
}

// TargetEndpoint identifies a caller-owned service that an interaction engine
// can attach to, such as a Chromium CDP endpoint or a Unity bridge endpoint.
type TargetEndpoint struct {
	Name          string
	Protocol      string
	URL           string
	CredentialRef string
	TLSRef        string
	Audience      string
	Metadata      map[string]string
	// Connection is resolved in-memory connection material. It is never part
	// of the provider protocol, manifests, events, logs, or API responses.
	Connection *EndpointConnection
}

// EndpointConnection carries secret connection material to an adapter after
// reference resolution. Adapters must not include any of these values in
// errors, events, command arguments, or persisted state.
type EndpointConnection struct {
	Headers   map[string]string
	TLS       *TLSConnection
	ExpiresAt time.Time

	mu                  sync.RWMutex
	credentialExpiresAt time.Time
	tlsExpiresAt        time.Time
	revision            uint64
	credentialRevision  uint64
	tlsRevision         uint64
	updates             chan uint64
	acknowledged        uint64
	acknowledgements    chan uint64
	secretValues        map[string]struct{}
	secretOrder         []string
}

type EndpointConnectionSnapshot struct {
	Headers            map[string]string
	TLS                *TLSConnection
	ExpiresAt          time.Time
	Revision           uint64
	CredentialRevision uint64
	TLSRevision        uint64
	SecretValues       []string
}

func (c *EndpointConnection) Snapshot() EndpointConnectionSnapshot {
	if c == nil {
		return EndpointConnectionSnapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	secrets := make([]string, 0, len(c.secretValues))
	for value := range c.secretValues {
		secrets = append(secrets, value)
	}
	return EndpointConnectionSnapshot{
		Headers: cloneConnectionValues(c.Headers), TLS: cloneTLSConnection(c.TLS),
		ExpiresAt: c.ExpiresAt, Revision: c.revision,
		CredentialRevision: c.credentialRevision, TLSRevision: c.tlsRevision,
		SecretValues: secrets,
	}
}

func (c *EndpointConnection) Updates() <-chan uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.updates == nil {
		c.updates = make(chan uint64, 1)
	}
	return c.updates
}

func (c *EndpointConnection) ReplaceCredential(headers map[string]string, expiresAt time.Time) uint64 {
	c.mu.Lock()
	c.Headers = cloneConnectionValues(headers)
	if c.secretValues == nil {
		c.secretValues = make(map[string]struct{})
	}
	for _, value := range headers {
		if value != "" {
			if _, exists := c.secretValues[value]; !exists {
				c.secretValues[value] = struct{}{}
				c.secretOrder = append(c.secretOrder, value)
			}
		}
	}
	for len(c.secretOrder) > 32 {
		delete(c.secretValues, c.secretOrder[0])
		c.secretOrder = c.secretOrder[1:]
	}
	c.credentialExpiresAt = expiresAt
	c.recomputeExpiryLocked()
	c.revision++
	c.credentialRevision++
	c.notifyLocked()
	revision := c.revision
	c.mu.Unlock()
	return revision
}

func (c *EndpointConnection) ReplaceTLS(material *TLSConnection, expiresAt time.Time) uint64 {
	c.mu.Lock()
	c.TLS = cloneTLSConnection(material)
	c.tlsExpiresAt = expiresAt
	c.recomputeExpiryLocked()
	c.revision++
	c.tlsRevision++
	c.notifyLocked()
	revision := c.revision
	c.mu.Unlock()
	return revision
}

func (c *EndpointConnection) Acknowledge(revision uint64) {
	c.mu.Lock()
	if revision > c.acknowledged {
		c.acknowledged = revision
	}
	if c.acknowledgements == nil {
		c.acknowledgements = make(chan uint64, 1)
	}
	select {
	case c.acknowledgements <- c.acknowledged:
	default:
		select {
		case <-c.acknowledgements:
		default:
		}
		c.acknowledgements <- c.acknowledged
	}
	c.mu.Unlock()
}

func (c *EndpointConnection) Acknowledgements() (<-chan uint64, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.acknowledgements == nil {
		c.acknowledgements = make(chan uint64, 1)
	}
	return c.acknowledgements, c.acknowledged
}

func (c *EndpointConnection) Clear() {
	c.mu.Lock()
	for name := range c.Headers {
		delete(c.Headers, name)
	}
	c.Headers = nil
	c.TLS = nil
	c.ExpiresAt = time.Time{}
	c.credentialExpiresAt = time.Time{}
	c.tlsExpiresAt = time.Time{}
	c.secretValues = nil
	c.secretOrder = nil
	c.revision++
	c.credentialRevision++
	c.tlsRevision++
	c.notifyLocked()
	c.mu.Unlock()
}

func (c *EndpointConnection) recomputeExpiryLocked() {
	c.ExpiresAt = c.credentialExpiresAt
	if c.ExpiresAt.IsZero() || !c.tlsExpiresAt.IsZero() && c.tlsExpiresAt.Before(c.ExpiresAt) {
		c.ExpiresAt = c.tlsExpiresAt
	}
}

func (c *EndpointConnection) notifyLocked() {
	if c.updates == nil {
		return
	}
	select {
	case c.updates <- c.revision:
	default:
		select {
		case <-c.updates:
		default:
		}
		c.updates <- c.revision
	}
}

func cloneConnectionValues(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for name, value := range values {
		result[name] = value
	}
	return result
}

func cloneTLSConnection(value *TLSConnection) *TLSConnection {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

// TLSConnection references caller-managed TLS files. The resolver owns their
// lifecycle; adapters may read them only while the engine instance is alive.
type TLSConnection struct {
	CAFile                string
	ClientCertificateFile string
	ClientKeyFile         string
	ServerName            string
}

// EngineHandles contains opaque target handles supplied and owned by the
// caller. An adapter may interpret a handle it understands, but never owns the
// underlying resource.
type EngineHandles map[string]string

// EngineTarget is the complete caller-resolved target description. A person,
// native launcher, container/VM manager, Xallet, or another target owner may
// supply it. Location and lifecycle are deliberately outside this contract.
type EngineTarget struct {
	APIVersion string
	TargetID   string
	Kind       string
	Endpoints  []TargetEndpoint
	Handles    EngineHandles
	Metadata   map[string]string
}

func (t EngineTarget) Endpoint(protocol string) (TargetEndpoint, bool) {
	for _, endpoint := range t.Endpoints {
		if endpoint.Protocol == protocol {
			return endpoint, true
		}
	}
	return TargetEndpoint{}, false
}

// EngineEvent reports an interaction-session lifecycle or health transition.
type EngineEvent struct {
	Type       string
	Status     string
	Message    string
	OccurredAt time.Time
}

type EngineEventSource interface {
	EngineEvents() <-chan EngineEvent
}

type EngineHealth struct {
	Status     string
	Message    string
	ObservedAt time.Time
}

const (
	EngineHealthStarting  = "connecting"
	EngineHealthStopping  = "disconnecting"
	EngineHealthHealthy   = "connected"
	EngineHealthUnhealthy = "unhealthy"
	EngineHealthStopped   = "disconnected"
	EngineHealthUnknown   = "unknown"
)

type EngineHealthProvider interface {
	EngineHealth(context.Context) EngineHealth
}

type EngineCapabilityProvider interface {
	EngineCapabilities() []string
}

type EngineInspection struct {
	Available    bool
	Capabilities []string
	Message      string
}

// EngineInspector reports interaction-adapter availability and capabilities.
type EngineInspector interface {
	InspectEngine(context.Context) EngineInspection
}

// EngineAdapter connects a Jangolova interaction engine to a caller-owned
// target. It does not launch, stop, or otherwise provision that target.
type EngineAdapter interface {
	Connect(context.Context, manifest.EngineSpec, EngineTarget) (EngineInstance, error)
}
