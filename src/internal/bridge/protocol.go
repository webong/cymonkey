// Package bridge defines the engine-neutral wire contract implemented by
// cooperative browser experiences and future native engine plugins.
package bridge

import (
	"encoding/json"
	"time"
)

const ProtocolVersion = "jangolova.bridge/v1alpha1"

const (
	MethodHello        = "hello"
	MethodCapabilities = "capabilities"
	MethodDescribe     = "describe"
	MethodAct          = "act"
	MethodEvents       = "events"
)

// Hello identifies a bridge implementation and negotiates the protocol.
type Hello struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Implementation  Implementation `json:"implementation"`
	Features        []string       `json:"features,omitempty"`
}

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type Effect string

const (
	EffectRead     Effect = "read"
	EffectWrite    Effect = "write"
	EffectExternal Effect = "external"
)

// Capability describes one bounded operation implemented cooperatively by an
// engine experience. It is descriptive protocol data, not authorization.
type Capability struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Effect      Effect          `json:"effect"`
}

type Event struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurredAt"`
	Data       json.RawMessage `json:"data,omitempty"`
}

// EventQuery is the wire request for a cursor-based event read.
type EventQuery struct {
	After string   `json:"after,omitempty"`
	Types []string `json:"types,omitempty"`
	Limit int      `json:"limit,omitempty"`
}

// EventBatch is the bridge wire response for an event read.
type EventBatch struct {
	Events []Event `json:"events"`
	Cursor string  `json:"cursor"`
}
