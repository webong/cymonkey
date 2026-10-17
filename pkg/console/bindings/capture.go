package bindings

import (
	"context"
	"errors"
	"time"

	"board/capture"
	"board/display/ui"
	"console"
)

type SeeResult struct {
	Capture     capture.Snapshot `json:"capture"`
	Observation ui.Observation   `json:"observation"`
	Consistency string           `json:"consistency"` // sequential reads; not atomic
}

// See combines pixels and semantics for one host-selected window. The native
// producers may differ, but process generation and window identity must agree.
// Consumers may remove bytes after storing them and add their own references.
func See(definition console.Definition, observer ui.Observer, surfaceID string, bridge capture.Bridge) (console.Tool, error) {
	if observer == nil || surfaceID == "" || bridge.Caller == nil || bridge.SurfaceID == "" {
		return console.Tool{}, errors.New("see needs bound observation and capture sources")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, selector ui.Selector) (SeeResult, error) {
		var result SeeResult
		shot, err := bridge.Snapshot(ctx)
		if err != nil {
			return result, err
		}
		if selector.WindowID != "" && selector.WindowID != shot.Target.WindowID {
			return result, errors.New("selector differs from bound capture window")
		}
		selector.WindowID = shot.Target.WindowID
		request := ui.ObservationRequest{SurfaceID: surfaceID, Selector: selector}
		observed, err := observer.Observe(ctx, request)
		if err != nil {
			return result, err
		}
		if err := ui.ValidateObservation(observed, request, time.Now()); err != nil {
			return result, err
		}
		if observed.Target.ProcessGeneration != shot.Target.ProcessGeneration || observed.Target.WindowID != shot.Target.WindowID {
			return result, errors.New("observation and capture targets differ")
		}
		result = SeeResult{Capture: shot, Observation: observed, Consistency: "sequential"}
		return result, nil
	})
}
