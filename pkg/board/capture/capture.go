// Package capture provides bounded frame sampling and capture manifests.
// Callers supply capture and storage functions; no device or file is opened.
package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"math"
	"time"
)

type Frame struct {
	Data            []byte
	ObservedAt      time.Time
	Target          string
	CoordinateSpace string
}
type Source interface {
	Capture(context.Context) (Frame, error)
}
type SourceFunc func(context.Context) (Frame, error)

func (f SourceFunc) Capture(ctx context.Context) (Frame, error) { return f(ctx) }

type Region struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
type Metadata struct {
	Sequence        int       `json:"sequence"`
	ObservedAt      time.Time `json:"observedAt"`
	Target          string    `json:"target"`
	CoordinateSpace string    `json:"coordinateSpace"`
	Width           int       `json:"width"`
	Height          int       `json:"height"`
	Bytes           int       `json:"bytes"`
	SHA256          string    `json:"sha256"`
	ChangedFraction float64   `json:"changedFraction"`
	ChangedBounds   *Region   `json:"changedBounds,omitempty"`
	Reference       string    `json:"reference,omitempty"`
}
type Manifest struct {
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   time.Time  `json:"finishedAt"`
	Sampled      int        `json:"sampled"`
	Retained     int        `json:"retained"`
	SampledBytes int64      `json:"sampledBytes"`
	Frames       []Metadata `json:"frames"`
	Complete     bool       `json:"complete"`
	StopReason   string     `json:"stopReason"`
}
type Options struct {
	MaxFrames     int
	MaxDuration   time.Duration
	Interval      time.Duration
	Heartbeat     time.Duration
	MaxFrameBytes int
	MaxTotalBytes int64
	MaxPixels     int
	MinChange     float64
}

// Consumer returns a host-owned stored reference or an empty reference when
// it only displayed/processed a frame. Stored bytes and reference custody are
// the caller's responsibility; the sampler creates no files or child process.
type Consumer func(context.Context, Frame, Metadata) (string, error)

func (o Options) Validate() error {
	if o.MaxFrames < 1 || o.MaxFrames > 10000 || o.MaxDuration <= 0 || o.MaxDuration > time.Hour || o.Interval < 20*time.Millisecond || o.Interval > o.MaxDuration || o.Heartbeat < 0 || o.Heartbeat > o.MaxDuration || o.MaxFrameBytes < 1 || o.MaxFrameBytes > 64<<20 || o.MaxTotalBytes < int64(o.MaxFrameBytes) || o.MaxTotalBytes > 1<<30 || o.MaxPixels < 1 || o.MaxPixels > 16<<20 || math.IsNaN(o.MinChange) || math.IsInf(o.MinChange, 0) || o.MinChange < 0 || o.MinChange > 1 {
		return errors.New("invalid capture sampling bounds")
	}
	return nil
}

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

// Difference measures pixel changes between equal-sized frames. A resize
// reports a completely changed image. Bounds use image-local pixels.
func Difference(previous, current image.Image) (float64, *Region) {
	b := current.Bounds()
	if previous == nil || previous.Bounds().Size() != b.Size() {
		return 1, &Region{0, 0, b.Dx(), b.Dy()}
	}
	p := previous.Bounds()
	count := 0
	minX, minY, maxX, maxY := b.Dx(), b.Dy(), -1, -1
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := current.At(b.Min.X+x, b.Min.Y+y).RGBA()
			pr, pg, pb, pa := previous.At(p.Min.X+x, p.Min.Y+y).RGBA()
			if r != pr || g != pg || bl != pb || a != pa {
				count++
				minX = min(minX, x)
				minY = min(minY, y)
				maxX = max(maxX, x)
				maxY = max(maxY, y)
			}
		}
	}
	if count == 0 {
		return 0, nil
	}
	return float64(count) / float64(b.Dx()*b.Dy()), &Region{minX, minY, maxX - minX + 1, maxY - minY + 1}
}

// Sample performs read-only captures under explicit time/frame/byte limits.
// Manifest metrics count all samples, including frames the consumer discarded.
func Sample(ctx context.Context, source Source, options Options, consume Consumer) (manifest Manifest, err error) {
	manifest.StartedAt = time.Now().UTC()
	defer func() { manifest.FinishedAt = time.Now().UTC() }()
	if source == nil || consume == nil {
		return manifest, errors.New("capture source and consumer are required")
	}
	if err = options.Validate(); err != nil {
		return manifest, err
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, options.MaxDuration)
	defer cancel()
	var previous image.Image
	var target, coordinateSpace string
	var retainedAt time.Time
	for sequence := 0; sequence < options.MaxFrames; sequence++ {
		if err = ctx.Err(); err != nil {
			manifest.StopReason = "duration"
			if parent.Err() == nil {
				manifest.Complete = true
				return manifest, nil
			}
			manifest.StopReason = "canceled"
			return manifest, parent.Err()
		}
		frame, e := source.Capture(ctx)
		if e != nil {
			manifest.StopReason = "capture_failed"
			return manifest, e
		}
		img, e := decode(frame, options.MaxFrameBytes, options.MaxPixels)
		if e != nil {
			manifest.StopReason = "invalid_frame"
			return manifest, e
		}
		if target == "" {
			target, coordinateSpace = frame.Target, frame.CoordinateSpace
		}
		if frame.Target != target || frame.CoordinateSpace != coordinateSpace {
			manifest.StopReason = "target_changed"
			return manifest, errors.New("capture target changed")
		}
		if manifest.SampledBytes+int64(len(frame.Data)) > options.MaxTotalBytes {
			manifest.StopReason = "byte_limit"
			manifest.Complete = true
			return manifest, nil
		}
		manifest.Sampled++
		manifest.SampledBytes += int64(len(frame.Data))
		fraction, region := Difference(previous, img)
		if previous == nil || fraction > 0 && fraction >= options.MinChange || options.Heartbeat > 0 && time.Since(retainedAt) >= options.Heartbeat {
			digest := sha256.Sum256(frame.Data)
			bounds := img.Bounds()
			meta := Metadata{Sequence: sequence, ObservedAt: frame.ObservedAt, Target: frame.Target, CoordinateSpace: frame.CoordinateSpace, Width: bounds.Dx(), Height: bounds.Dy(), Bytes: len(frame.Data), SHA256: hex.EncodeToString(digest[:]), ChangedFraction: fraction, ChangedBounds: region}
			ref, e := consume(ctx, frame, meta)
			if e != nil {
				manifest.StopReason = "consumer_failed"
				return manifest, e
			}
			meta.Reference = ref
			manifest.Frames = append(manifest.Frames, meta)
			manifest.Retained++
			retainedAt = time.Now()
			previous = img
		}
		if sequence+1 == options.MaxFrames {
			break
		}
		timer := time.NewTimer(options.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	manifest.Complete = true
	manifest.StopReason = "frame_limit"
	return manifest, nil
}

// ContactSheet creates a finite PNG from caller-owned frames. Tile positions
// and dimensions are deterministic; annotations remain a presentation concern.
func ContactSheet(frames []Frame, columns, tileWidth, tileHeight, maxPixels int) ([]byte, error) {
	if len(frames) == 0 || len(frames) > 256 || columns < 1 || columns > len(frames) || tileWidth < 1 || tileHeight < 1 || tileWidth > 4096 || tileHeight > 4096 || maxPixels < 1 || maxPixels > 16<<20 {
		return nil, errors.New("invalid contact sheet bounds")
	}
	rows := (len(frames) + columns - 1) / columns
	width, height := columns*tileWidth, rows*tileHeight
	if width > maxPixels/height {
		return nil, errors.New("contact sheet exceeds pixel ceiling")
	}
	output := image.NewRGBA(image.Rect(0, 0, width, height))
	for i, frame := range frames {
		img, err := decode(frame, 64<<20, maxPixels)
		if err != nil {
			return nil, err
		}
		b := img.Bounds()
		x0, y0 := (i%columns)*tileWidth, (i/columns)*tileHeight
		for y := 0; y < tileHeight; y++ {
			for x := 0; x < tileWidth; x++ {
				output.Set(x0+x, y0+y, img.At(b.Min.X+x*b.Dx()/tileWidth, b.Min.Y+y*b.Dy()/tileHeight))
			}
		}
	}
	var data bytes.Buffer
	err := png.Encode(&data, output)
	return data.Bytes(), err
}
