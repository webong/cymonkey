package ui

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/webong/ext/pkg/plugin/jsonschema"
)

type VerificationState string

const (
	Satisfied   VerificationState = "satisfied"
	Unsatisfied VerificationState = "unsatisfied"
	Unknown     VerificationState = "unknown"
)

type Predicate struct {
	Condition string          `json:"condition"` // exists, absent, equals
	Attribute string          `json:"attribute,omitempty"`
	Expected  json.RawMessage `json:"expected,omitempty"`
}
type Verification struct {
	State       VerificationState `json:"state"`
	Observation *Observation      `json:"observation,omitempty"`
	Attempts    int               `json:"attempts"`
	Reason      string            `json:"reason,omitempty"`
}
type WaitOptions struct {
	Timeout        time.Duration
	Interval       time.Duration
	StableFor      time.Duration
	ExpectedTarget *TargetRef
}

// Evaluate applies a predicate to an observation of the requested selector.
// Incomplete observations can prove presence, but never absence.
func Evaluate(o Observation, p Predicate) VerificationState {
	switch p.Condition {
	case "exists":
		if len(o.Elements) > 0 {
			return Satisfied
		}
		if o.Completeness == "complete" {
			return Unsatisfied
		}
	case "absent":
		if len(o.Elements) > 0 {
			return Unsatisfied
		}
		if o.Completeness == "complete" {
			return Satisfied
		}
	case "equals":
		if p.Attribute == "" || !json.Valid(p.Expected) {
			return Unknown
		}
		schema, _ := json.Marshal(map[string]any{"const": p.Expected})
		missing := false
		for _, e := range o.Elements {
			v, ok := e.Attributes[p.Attribute]
			if !ok {
				missing = true
				continue
			}
			if jsonschema.Validate(schema, v) == nil {
				return Satisfied
			}
		}
		if !missing && o.Completeness == "complete" {
			return Unsatisfied
		}
	}
	return Unknown
}

// Verify performs a single read. It never changes focus, invokes a tool or
// uses a model to decide whether a predicate holds.
func Verify(ctx context.Context, observer Observer, request ObservationRequest, p Predicate) (Verification, error) {
	r := Verification{State: Unknown, Attempts: 1}
	if observer == nil || request.SurfaceID == "" || request.Selector.Limit < 0 || request.Selector.Limit > 500 {
		return r, errors.New("invalid UI verification request")
	}
	if p.Condition != "exists" && p.Condition != "absent" && p.Condition != "equals" || p.Condition == "equals" && (p.Attribute == "" || !json.Valid(p.Expected)) {
		return r, errors.New("invalid UI predicate")
	}
	o, err := observer.Observe(ctx, request)
	if err != nil {
		r.Reason = "observation failed"
		return r, err
	}
	if err = ValidateObservation(o, request, time.Now()); err != nil {
		r.Reason = "invalid observation"
		return r, err
	}
	r.Observation = &o
	r.State = Evaluate(o, p)
	if r.State == Unknown {
		r.Reason = o.Reason
	}
	return r, nil
}

// Wait reads fresh observations until satisfied for StableFor or Timeout.
// The first resolved target is pinned; a process/window replacement is unknown.
func Wait(ctx context.Context, observer Observer, request ObservationRequest, p Predicate, options WaitOptions) (Verification, error) {
	result := Verification{State: Unknown}
	if options.Timeout <= 0 || options.Timeout > 5*time.Minute || options.Interval < 20*time.Millisecond || options.Interval > options.Timeout || options.StableFor < 0 || options.StableFor > options.Timeout {
		return result, errors.New("invalid UI wait bounds")
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	var target TargetRef
	if options.ExpectedTarget != nil {
		target = *options.ExpectedTarget
	}
	var since time.Time
	for attempts := 1; ; attempts++ {
		current, err := Verify(ctx, observer, request, p)
		current.Attempts = attempts
		if err != nil {
			current.State = Unknown
			return current, err
		}
		if target.SurfaceID == "" {
			target = current.Observation.Target
		}
		if current.Observation.Target != target {
			current.State = Unknown
			current.Reason = "observed target changed"
			return current, errors.New("UI target changed during verification")
		}
		if current.State == Satisfied {
			if since.IsZero() {
				since = time.Now()
			}
			if time.Since(since) >= options.StableFor {
				return current, nil
			}
		} else {
			since = time.Time{}
		}
		result = current
		timer := time.NewTimer(options.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			result.State = Unknown
			result.Reason = "verification deadline or cancellation"
			return result, ctx.Err()
		case <-timer.C:
		}
	}
}
