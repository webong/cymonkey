package bindings

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"blockade"
	"console"
	"jangolova/ui"
)

// Observe binds a surface to a read-only observer. Tool input selects a query,
// never a new surface or runtime. The observer owns per-call authorization.
func Observe(definition console.Definition, observer ui.Observer, surfaceID string) (console.Tool, error) {
	if observer == nil || surfaceID == "" {
		return console.Tool{}, errors.New("observer and fixed surface are required")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, selector ui.Selector) (ui.Observation, error) {
		request := ui.ObservationRequest{SurfaceID: surfaceID, Selector: selector}
		result, err := observer.Observe(ctx, request)
		if err != nil {
			return result, err
		}
		return result, ui.ValidateObservation(result, request, time.Now())
	})
}

type VerificationInput struct {
	Selector  ui.Selector  `json:"selector"`
	Predicate ui.Predicate `json:"predicate"`
}

func Verify(definition console.Definition, observer ui.Observer, surfaceID string) (console.Tool, error) {
	if observer == nil || surfaceID == "" {
		return console.Tool{}, errors.New("observer and fixed surface are required")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, input VerificationInput) (ui.Verification, error) {
		return ui.Verify(ctx, observer, ui.ObservationRequest{SurfaceID: surfaceID, Selector: input.Selector}, input.Predicate)
	})
}

type WaitInput struct {
	Selector       ui.Selector   `json:"selector"`
	Predicate      ui.Predicate  `json:"predicate"`
	TimeoutMS      int64         `json:"timeoutMs"`
	IntervalMS     int64         `json:"intervalMs"`
	StableForMS    int64         `json:"stableForMs,omitempty"`
	ExpectedTarget *ui.TargetRef `json:"expectedTarget,omitempty"`
}

// Wait repeatedly reads one fixed surface. It never invokes a mutating action.
func Wait(definition console.Definition, observer ui.Observer, surfaceID string) (console.Tool, error) {
	if observer == nil || surfaceID == "" {
		return console.Tool{}, errors.New("wait needs an observer and fixed surface")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, input WaitInput) (ui.Verification, error) {
		if input.TimeoutMS <= 0 || input.TimeoutMS > 300000 || input.IntervalMS < 20 || input.IntervalMS > input.TimeoutMS || input.StableForMS < 0 || input.StableForMS > input.TimeoutMS {
			return ui.Verification{State: ui.Unknown}, errors.New("invalid UI wait bounds")
		}
		return ui.Wait(ctx, observer, ui.ObservationRequest{SurfaceID: surfaceID, Selector: input.Selector}, input.Predicate, ui.WaitOptions{Timeout: time.Duration(input.TimeoutMS) * time.Millisecond, Interval: time.Duration(input.IntervalMS) * time.Millisecond, StableFor: time.Duration(input.StableForMS) * time.Millisecond, ExpectedTarget: input.ExpectedTarget})
	})
}

type ObservedActionInput struct {
	Observation ui.Observation `json:"observation"`
	Arguments   map[string]any `json:"arguments"`
}

func ObservedAction(definition console.Definition, client ui.Client, action string) (console.Tool, error) {
	if client.Caller == nil || client.SurfaceID == "" || action == "" {
		return console.Tool{}, errors.New("observed action needs a bound client and action")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, input ObservedActionInput) (ui.ActionReceipt, error) {
		return client.Invoke(ctx, input.Observation, action, input.Arguments)
	})
}

type EmbeddingInput struct {
	Texts      []string `json:"texts,omitempty"`
	Tokens     [][]int  `json:"tokens,omitempty"`
	Dimensions int      `json:"dimensions,omitempty"`
}

func Embed(definition console.Definition, provider blockade.Embedder, model string) (console.Tool, error) {
	if provider == nil || model == "" {
		return console.Tool{}, errors.New("embedding needs a provider and fixed model")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, input EmbeddingInput) (blockade.EmbeddingResponse, error) {
		r := blockade.EmbeddingRequest{Model: model, Texts: input.Texts, Tokens: input.Tokens, Dimensions: input.Dimensions}
		if err := blockade.ValidateEmbeddingRequest(r); err != nil {
			return blockade.EmbeddingResponse{}, err
		}
		result, err := provider.Embed(ctx, r)
		if err != nil {
			return result, err
		}
		return result, blockade.ValidateEmbeddingResponse(result, r)
	})
}

// JSONResult returns the explicitly selected JSON representation of a result.
// It is useful when a host stores media itself and returns its own reference.
func JSONResult(value any) (json.RawMessage, error) { return json.Marshal(value) }
