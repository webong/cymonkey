package capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type Caller interface {
	Call(context.Context, string, json.RawMessage) (json.RawMessage, error)
}
type Target struct {
	SurfaceID         string `json:"surfaceId"`
	ProducerID        string `json:"producerId"`
	ProcessGeneration string `json:"processGeneration"`
	WindowID          string `json:"windowId"`
}
type Snapshot struct {
	Format          string    `json:"format"`
	Width           int       `json:"width"`
	Height          int       `json:"height"`
	Data            []byte    `json:"base64Data"`
	ID              string    `json:"captureId"`
	Target          Target    `json:"target"`
	ObservedAt      time.Time `json:"observedAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
	SHA256          string    `json:"sha256"`
	CoordinateSpace string    `json:"coordinateSpace"`
}

// Bridge borrows a native connection and captures only its host-bound surface.
// Snapshot IDs can be presented to native input actions as freshness evidence.
type Bridge struct {
	Caller    Caller
	SurfaceID string
	MaxBytes  int
	MaxPixels int
}

func (b Bridge) Snapshot(ctx context.Context) (Snapshot, error) {
	var result Snapshot
	if b.Caller == nil || b.SurfaceID == "" || b.MaxBytes <= 0 || b.MaxBytes > 64<<20 || b.MaxPixels < 1 || b.MaxPixels > 16<<20 {
		return result, errors.New("capture bridge needs a caller, fixed surface and byte/pixel ceilings")
	}
	params, _ := json.Marshal(map[string]any{"name": "display.capture", "input": map[string]string{"surfaceId": b.SurfaceID}})
	data, err := b.Caller.Call(ctx, "act", params)
	if err != nil {
		return result, err
	}
	if len(data) > b.MaxBytes*2+65536 {
		return result, errors.New("capture response exceeds byte ceiling")
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if result.ID == "" || result.Format != "image/png" || result.CoordinateSpace != "window-local-pixels" || result.Target.SurfaceID != b.SurfaceID || result.Target.ProducerID == "" || result.Target.ProcessGeneration == "" || result.Target.WindowID == "" || result.ObservedAt.IsZero() || !result.ExpiresAt.After(result.ObservedAt) || !result.ExpiresAt.After(time.Now()) || result.ObservedAt.After(time.Now().Add(5*time.Second)) {
		return result, errors.New("capture identity is invalid or expired")
	}
	digest := sha256.Sum256(result.Data)
	if result.SHA256 != hex.EncodeToString(digest[:]) {
		return result, errors.New("capture digest does not match bytes")
	}
	img, err := decode(Frame{Data: result.Data, Target: b.SurfaceID, ObservedAt: result.ObservedAt, CoordinateSpace: result.CoordinateSpace}, b.MaxBytes, b.MaxPixels)
	if err != nil {
		return result, err
	}
	if img.Bounds().Dx() != result.Width || img.Bounds().Dy() != result.Height {
		return result, errors.New("capture dimensions differ from metadata")
	}
	return result, nil
}
func (b Bridge) Capture(ctx context.Context) (Frame, error) {
	s, err := b.Snapshot(ctx)
	if err != nil {
		return Frame{}, err
	}
	target, _ := json.Marshal(s.Target)
	return Frame{Data: s.Data, ObservedAt: s.ObservedAt, Target: string(target), CoordinateSpace: s.CoordinateSpace}, nil
}

// Around captures before and after one caller-supplied operation. It returns
// post-action evidence even after an action error when the context permits it.
// The caller decides how to interpret the operation's dispatch uncertainty.
func Around(ctx context.Context, source Source, operation func(context.Context) error) (before, after Frame, err error) {
	if source == nil || operation == nil {
		return before, after, errors.New("capture source and explicit operation are required")
	}
	before, err = source.Capture(ctx)
	if err != nil {
		return before, after, err
	}
	actionErr := operation(ctx)
	after, captureErr := source.Capture(ctx)
	if captureErr == nil && after.Target != before.Target {
		captureErr = errors.New("capture target changed around operation")
	}
	return before, after, errors.Join(actionErr, captureErr)
}
