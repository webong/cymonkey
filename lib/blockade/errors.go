package blockade

import (
	"context"
	"errors"
	"time"
)

type ErrorKind string

const (
	ErrorInvalidInput    ErrorKind = "invalid_input"
	ErrorUnsupported     ErrorKind = "unsupported"
	ErrorAuthentication  ErrorKind = "authentication"
	ErrorRateLimit       ErrorKind = "rate_limit"
	ErrorUnavailable     ErrorKind = "unavailable"
	ErrorTransport       ErrorKind = "transport"
	ErrorCanceled        ErrorKind = "canceled"
	ErrorDeadline        ErrorKind = "deadline"
	ErrorInvalidResponse ErrorKind = "invalid_response"
)

// ModelError describes one failed operation. Retryable is advisory only;
// libraries never retry or choose another model on the caller's behalf.
type ModelError struct {
	Kind       ErrorKind     `json:"kind"`
	Operation  string        `json:"operation,omitempty"`
	Retryable  bool          `json:"retryable"`
	RetryAfter time.Duration `json:"retryAfter,omitempty"`
	Cause      error         `json:"-"`
}

func (e *ModelError) Error() string { return "model " + e.Operation + ": " + string(e.Kind) }
func (e *ModelError) Unwrap() error { return e.Cause }

func ClassifyModelError(err error) ErrorKind {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return ErrorCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorDeadline
	}
	var typed *ModelError
	if errors.As(err, &typed) {
		return typed.Kind
	}
	if errors.Is(err, ErrGenerationUnsupported) {
		return ErrorUnsupported
	}
	if errors.Is(err, ErrInvalidGeneration) {
		return ErrorInvalidInput
	}
	return ErrorTransport
}
