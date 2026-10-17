// Package ui provides portable observations, explicit actions and read-only
// verification over a caller-owned Jangolova connection.
package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"jangolova/contract"
	"jangolova/sdk"
)

type TargetRef struct {
	SurfaceID         string `json:"surfaceId"`
	ProducerID        string `json:"producerId"`
	ProcessGeneration string `json:"processGeneration"`
	WindowID          string `json:"windowId,omitempty"`
}
type Bounds struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type Element struct {
	ID                 string                     `json:"id"`
	SurfaceID          string                     `json:"surfaceId"`
	Role               string                     `json:"role,omitempty"`
	Title              string                     `json:"title,omitempty"`
	Identifier         string                     `json:"identifier,omitempty"`
	Bounds             *Bounds                    `json:"bounds,omitempty"`
	Attributes         map[string]json.RawMessage `json:"attributes,omitempty"`
	Actions            []string                   `json:"actions"`
	SettableAttributes []string                   `json:"settableAttributes"`
}
type Selector struct {
	Role       string `json:"role,omitempty"`
	Title      string `json:"title,omitempty"`
	Identifier string `json:"identifier,omitempty"`
	WindowID   string `json:"windowId,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}
type ObservationRequest struct {
	SurfaceID string   `json:"surfaceId"`
	Selector  Selector `json:"selector"`
}
type Observation struct {
	ID              string    `json:"id"`
	Target          TargetRef `json:"target"`
	ObservedAt      time.Time `json:"observedAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
	CoordinateSpace string    `json:"coordinateSpace"`
	Completeness    string    `json:"completeness"` // complete, partial, unknown for the requested selector
	Reason          string    `json:"reason,omitempty"`
	Visited         int       `json:"visited"`
	Elements        []Element `json:"elements"`
}
type Observer interface {
	Observe(context.Context, ObservationRequest) (Observation, error)
}

func ValidateObservation(o Observation, request ObservationRequest, now time.Time) error {
	if o.ID == "" || o.Target.SurfaceID != request.SurfaceID || o.Target.ProducerID == "" || o.Target.ProcessGeneration == "" || o.ObservedAt.IsZero() || !o.ExpiresAt.After(o.ObservedAt) || !o.ExpiresAt.After(now) || o.ObservedAt.After(now.Add(5*time.Second)) || len(o.Elements) > 500 || o.Visited < 0 {
		return errors.New("invalid or expired UI observation")
	}
	if request.Selector.WindowID != "" && o.Target.WindowID != request.Selector.WindowID {
		return errors.New("observation window differs from requested target")
	}
	switch o.Completeness {
	case "complete", "partial", "unknown":
	default:
		return errors.New("unknown observation completeness")
	}
	ids := map[string]bool{}
	for _, e := range o.Elements {
		if e.ID == "" || e.SurfaceID != o.Target.SurfaceID || ids[e.ID] {
			return errors.New("invalid UI element reference")
		}
		ids[e.ID] = true
	}
	return nil
}

// Client binds one surface. Requests cannot redirect it to another surface.
// Caller must enforce host authorization on every act call.
type Client struct {
	Caller    sdk.Caller
	SurfaceID string
}

func (c Client) Observe(ctx context.Context, request ObservationRequest) (Observation, error) {
	var result Observation
	if c.Caller == nil || c.SurfaceID == "" || request.SurfaceID != c.SurfaceID {
		return result, errors.New("UI observer needs its bound surface")
	}
	input, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	raw, err := json.Marshal(contract.Action{Name: "ui.observe", Input: input})
	if err != nil {
		return result, err
	}
	data, err := c.Caller.Call(ctx, sdk.MethodAct, raw)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	return result, ValidateObservation(result, request, time.Now())
}

type ActionReceipt struct {
	ID            string          `json:"id"`
	Action        string          `json:"action"`
	Target        TargetRef       `json:"target"`
	ObservationID string          `json:"observationId,omitempty"`
	Dispatch      string          `json:"dispatch"`     // dispatched, not_dispatched, unknown
	Verification  string          `json:"verification"` // unknown until separately verified
	OccurredAt    time.Time       `json:"occurredAt"`
	Output        json.RawMessage `json:"output,omitempty"`
}

// Invoke invokes one selected semantic action against an unexpired observation.
// Errors after sending a request may have unknown dispatch; callers must not
// infer that retrying a mutating operation is safe.
func (c Client) Invoke(ctx context.Context, observed Observation, name string, arguments map[string]any) (ActionReceipt, error) {
	r := ActionReceipt{Action: name, Target: observed.Target, ObservationID: observed.ID, Dispatch: "not_dispatched", Verification: "unknown", OccurredAt: time.Now().UTC()}
	if c.Caller == nil || c.SurfaceID == "" || observed.Target.SurfaceID != c.SurfaceID {
		return r, errors.New("UI action needs its bound surface")
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if err := ValidateObservation(observed, ObservationRequest{SurfaceID: c.SurfaceID}, time.Now()); err != nil {
		return r, err
	}
	input := make(map[string]any, len(arguments)+3)
	for k, v := range arguments {
		input[k] = v
	}
	input["surfaceId"] = c.SurfaceID
	input["observationId"] = observed.ID
	input["target"] = observed.Target
	encoded, err := json.Marshal(input)
	if err != nil {
		return r, err
	}
	data, err := json.Marshal(contract.Action{Name: name, Input: encoded})
	if err != nil {
		return r, err
	}
	r.Dispatch = "unknown"
	output, err := c.Caller.Call(ctx, sdk.MethodAct, data)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(output, &r); err != nil {
		return r, fmt.Errorf("invalid action receipt: %w", err)
	}
	if r.Action != name || r.Target != observed.Target || r.ObservationID != observed.ID || r.ID == "" || r.Dispatch != "dispatched" || r.Verification != "unknown" {
		return r, errors.New("action receipt does not match observed target")
	}
	return r, nil
}
