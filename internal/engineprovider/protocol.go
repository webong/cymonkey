// Package engineprovider implements the provider-neutral interaction-engine
// contract. Jangolova is one implementation and Xallet is one supported
// target provider.
package engineprovider

import (
	"encoding/json"
	"time"

	"cymonkey/internal/blockade"
)

const APIVersion = "interaction.engine/v1alpha1"
const TargetAPIVersion = "interaction.target/v1alpha1"

type EngineDescriptor struct {
	Adapter      string   `json:"adapter"`
	Available    bool     `json:"available"`
	Capabilities []string `json:"capabilities"`
	Message      string   `json:"message,omitempty"`
}

type EngineSpec struct {
	Adapter              string          `json:"adapter"`
	RequiredCapabilities []string        `json:"requiredCapabilities,omitempty"`
	Source               string          `json:"source,omitempty"`
	Options              json.RawMessage `json:"options,omitempty"`
	Approval             ApprovalPolicy  `json:"approval,omitempty"`
}

// ApprovalPolicy makes selected semantic actions require a separate,
// one-time owner approval receipt before execution.
type ApprovalPolicy struct {
	RequiredActions []string `json:"requiredActions,omitempty"`
}

type TargetEndpoint struct {
	Name          string            `json:"name"`
	Protocol      string            `json:"protocol"`
	URL           string            `json:"url"`
	CredentialRef string            `json:"credentialRef,omitempty"`
	TLSRef        string            `json:"tlsRef,omitempty"`
	Audience      string            `json:"audience,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type Target struct {
	APIVersion string            `json:"apiVersion,omitempty"`
	TargetID   string            `json:"targetId,omitempty"`
	Kind       string            `json:"kind"`
	Endpoints  []TargetEndpoint  `json:"endpoints,omitempty"`
	Handles    map[string]string `json:"handles,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type ConnectRequest struct {
	APIVersion string     `json:"apiVersion"`
	InstanceID string     `json:"instanceId"`
	Engine     EngineSpec `json:"engine"`
	Target     Target     `json:"target"`
}

type Instance struct {
	APIVersion   string        `json:"apiVersion"`
	InstanceID   string        `json:"instanceId"`
	Adapter      string        `json:"adapter"`
	Status       string        `json:"status"`
	Health       Health        `json:"health"`
	Capabilities []string      `json:"capabilities,omitempty"`
	CallerLaunch *CallerLaunch `json:"callerLaunch,omitempty"`
}

type CallerLaunch struct {
	Environment map[string]string `json:"environment"`
}

type Health struct {
	Status     string    `json:"status"`
	Message    string    `json:"message,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
}

type CallRequest struct {
	Method     string          `json:"method"`
	Params     json.RawMessage `json:"params,omitempty"`
	ApprovalID string          `json:"approvalId,omitempty"`
}

type CallResponse struct {
	APIVersion string          `json:"apiVersion"`
	InstanceID string          `json:"instanceId"`
	Result     json.RawMessage `json:"result"`
}

type InstanceEvent struct {
	Cursor     string    `json:"cursor,omitempty"`
	Type       string    `json:"type"`
	Status     string    `json:"status,omitempty"`
	Message    string    `json:"message,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

type InstanceEventBatch struct {
	APIVersion string          `json:"apiVersion"`
	InstanceID string          `json:"instanceId"`
	Events     []InstanceEvent `json:"events"`
	Cursor     string          `json:"cursor"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ApprovalRequest struct {
	Params           json.RawMessage `json:"params"`
	ExpiresInSeconds int             `json:"expiresInSeconds,omitempty"`
}

type ApprovalResolution struct {
	Approved bool `json:"approved"`
}

type Approval struct {
	APIVersion string    `json:"apiVersion"`
	InstanceID string    `json:"instanceId"`
	ApprovalID string    `json:"approvalId"`
	Action     string    `json:"action"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// ObserveRequest asks Jangolova to capture pixels from an attached target and
// submit them to Blockade. It is a Jangolova workflow request, not a Blockade
// protocol type.
type ObserveRequest struct {
	Prompt     string `json:"prompt,omitempty"`
	FullPage   bool   `json:"fullPage,omitempty"`
	ApprovalID string `json:"approvalId,omitempty"`
}

// ObservationResponse keeps target provenance outside Blockade's normalized
// response. Blockade receives pixels and returns observations only.
type ObservationResponse struct {
	APIVersion    string                   `json:"apiVersion"`
	InstanceID    string                   `json:"instanceId"`
	CapturedAt    time.Time                `json:"capturedAt"`
	CaptureAction string                   `json:"captureAction"`
	Observation   blockade.ObserveResponse `json:"observation"`
}

type ReconcileRequest struct {
	APIVersion string           `json:"apiVersion,omitempty"`
	Prune      bool             `json:"prune,omitempty"`
	Desired    []ConnectRequest `json:"desired"`
}

type ReconcileResponse struct {
	APIVersion string            `json:"apiVersion"`
	Reconciled int               `json:"reconciled"`
	Created    []string          `json:"created,omitempty"`
	Retained   []string          `json:"retained,omitempty"`
	Pruned     []string          `json:"pruned,omitempty"`
	Failed     map[string]string `json:"failed,omitempty"`
}
