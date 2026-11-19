// Package capture binds the Console to a native viewer's host-bound surface.
// Frame sampling, change detection, crop, annotation, contact sheets and video
// sampling are ext's res/system/capture/media; a Bridge is a media.FrameSource.
package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"time"

	"github.com/webong/ext/res/system/capture/media"
)

// Frame is ext's captured frame.
type Frame = media.Frame

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

var _ media.FrameSource = Bridge{}

func (b Bridge) Capture(ctx context.Context) (Frame, error) {
	s, err := b.Snapshot(ctx)
	if err != nil {
		return Frame{}, err
	}
	target, _ := json.Marshal(s.Target)
	return Frame{Data: s.Data, ObservedAt: s.ObservedAt, Target: string(target), CoordinateSpace: s.CoordinateSpace}, nil
}

// decode accepts one bounded PNG frame with its capture identity.
func decode(frame Frame, maxBytes, maxPixels int) (image.Image, error) {
	if maxBytes < 1 || maxBytes > 64<<20 || maxPixels < 1 || maxPixels > 16<<20 || len(frame.Data) == 0 || len(frame.Data) > maxBytes || frame.ObservedAt.IsZero() || frame.Target == "" || frame.CoordinateSpace == "" {
		return nil, errors.New("invalid capture frame")
	}
	config, err := png.DecodeConfig(bytes.NewReader(frame.Data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > maxPixels/config.Height {
		return nil, errors.New("capture image exceeds dimensions or is not PNG")
	}
	return png.Decode(bytes.NewReader(frame.Data))
}
