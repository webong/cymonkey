package contentkit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	ProviderAdapterAPIVersion = "content.adapter/v1"

	CapabilityImageObserve   = "image.observe"
	CapabilityObjectDetect   = "object.detect"
	CapabilityObjectSegment  = "object.segment"
	CapabilityVisionLanguage = "vision.language"
)

// ProviderAdapterObserveRequest is the image observation boundary. The nested
// request remains the public content.observation/v1 request.
type ProviderAdapterObserveRequest struct {
	APIVersion string         `json:"apiVersion"`
	Request    ObserveRequest `json:"request"`
}

// ProviderAdapterObserveResponse wraps the normalized Blockade response so
// the adapter protocol can evolve independently from the observation schema.
type ProviderAdapterObserveResponse struct {
	APIVersion string          `json:"apiVersion"`
	Response   ObserveResponse `json:"response"`
}

// ProviderAdapterInterpretRequest and Response carry the separate content
// contract through provider adapters without changing the image wire format.
type ProviderAdapterInterpretRequest struct {
	APIVersion string           `json:"apiVersion"`
	Request    InterpretRequest `json:"request"`
}

type ProviderAdapterInterpretResponse struct {
	APIVersion string            `json:"apiVersion"`
	Response   InterpretResponse `json:"response"`
}

type ProviderAdapterCapabilities struct {
	APIVersion   string   `json:"apiVersion"`
	Capabilities []string `json:"capabilities"`
}

type ProviderAdapterHealth struct {
	APIVersion string `json:"apiVersion"`
	Ready      bool   `json:"ready"`
	Detail     string `json:"detail,omitempty"`
}

// ProviderAdapter keeps the existing image observation entry point for source
// compatibility. Content-only adapters return ErrModalityUnsupported from
// Observe and advertise only their supported content capabilities.
type ProviderAdapter interface {
	Observe(context.Context, ProviderAdapterObserveRequest) (ProviderAdapterObserveResponse, error)
	Capabilities(context.Context) (ProviderAdapterCapabilities, error)
	Health(context.Context) (ProviderAdapterHealth, error)
	Close() error
}

type ContentProviderAdapter interface {
	Interpret(context.Context, ProviderAdapterInterpretRequest) (ProviderAdapterInterpretResponse, error)
}

type ProviderAdapterErrorKind string

const (
	ProviderAdapterErrorAuthentication  ProviderAdapterErrorKind = "authentication"
	ProviderAdapterErrorRateLimit       ProviderAdapterErrorKind = "rate_limit"
	ProviderAdapterErrorTimeout         ProviderAdapterErrorKind = "timeout"
	ProviderAdapterErrorCanceled        ProviderAdapterErrorKind = "canceled"
	ProviderAdapterErrorUnavailable     ProviderAdapterErrorKind = "unavailable"
	ProviderAdapterErrorInvalidRequest  ProviderAdapterErrorKind = "invalid_request"
	ProviderAdapterErrorInvalidResponse ProviderAdapterErrorKind = "invalid_response"
)

// ProviderAdapterError preserves machine-readable failure categories without
// exposing provider responses or secret values through its public message.
type ProviderAdapterError struct {
	Kind       ProviderAdapterErrorKind
	AdapterID  string
	RetryAfter time.Duration
	cause      error
}

func NewProviderAdapterError(kind ProviderAdapterErrorKind, cause error) *ProviderAdapterError {
	return NewProviderAdapterErrorFor("", kind, cause)
}

func NewProviderAdapterErrorFor(adapterID string, kind ProviderAdapterErrorKind, cause error) *ProviderAdapterError {
	return &ProviderAdapterError{Kind: kind, AdapterID: adapterID, cause: cause}
}

func (e *ProviderAdapterError) Error() string {
	if e == nil {
		return "Blockade provider adapter failed"
	}
	if e.AdapterID == "" {
		return fmt.Sprintf("Blockade provider adapter failed: %s", e.Kind)
	}
	return fmt.Sprintf("Blockade provider adapter %q failed: %s", e.AdapterID, e.Kind)
}

func (e *ProviderAdapterError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func ProviderAdapterErrorKindOf(err error) (ProviderAdapterErrorKind, bool) {
	var adapterErr *ProviderAdapterError
	if !errors.As(err, &adapterErr) {
		return "", false
	}
	return adapterErr.Kind, true
}

func ValidProviderAdapterErrorKind(kind ProviderAdapterErrorKind) bool {
	switch kind {
	case ProviderAdapterErrorAuthentication,
		ProviderAdapterErrorRateLimit,
		ProviderAdapterErrorTimeout,
		ProviderAdapterErrorCanceled,
		ProviderAdapterErrorUnavailable,
		ProviderAdapterErrorInvalidRequest,
		ProviderAdapterErrorInvalidResponse:
		return true
	default:
		return false
	}
}

// ProviderAdapterRuntime contains non-secret settings and a lazy secret source.
// Secret values never appear in the YAML-backed configuration value.
type ProviderAdapterRuntime struct {
	ID       string
	Workers  int
	Settings map[string]string
	Model    *InstalledModel
	Secrets  ProviderAdapterSecrets
	// SecretNames lists only configured logical secret names, allowing an
	// external adapter to request those values without seeing host variables.
	SecretNames []string
}

type ProviderAdapterSecrets interface {
	Resolve(context.Context, string) (string, error)
}

// The content.provider contract: the name, version and operations an ext package
// declares when it serves content providers.
const (
	ProviderContract        = "content.provider"
	ProviderContractVersion = "v1"
)

// ProviderOperations lists every method a content.provider package serves.
var ProviderOperations = []string{"start", "observe", "interpret", "capabilities", "health", "close", "model.inspect"}
