package capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// VideoDecoder borrows a host-selected decoder and media resource. It does not
// open paths, fetch URLs or launch a codec. FrameAt must honor cancellation and
// impose its own decoding bounds before returning an encoded PNG frame.
type VideoDecoder interface {
	FrameAt(context.Context, time.Duration) (Frame, error)
}
type VideoOptions struct {
	Offsets       []time.Duration
	MaxDuration   time.Duration
	MaxFrameBytes int
	MaxTotalBytes int64
	MaxPixels     int
}
type VideoFrame struct {
	OffsetMS int64    `json:"offsetMs"`
	Metadata Metadata `json:"metadata"`
}
type VideoManifest struct {
	Frames   []VideoFrame `json:"frames"`
	Bytes    int64        `json:"bytes"`
	Complete bool         `json:"complete"`
}

// SampleVideo extracts only caller-selected timestamps, once each. The host
// decides whether to retain bytes and whether a failed sample may be requested
// again. Decoders and storage stay open for their owner.
func SampleVideo(ctx context.Context, decoder VideoDecoder, options VideoOptions, consume Consumer) (VideoManifest, error) {
	var result VideoManifest
	if decoder == nil || consume == nil || len(options.Offsets) < 1 || len(options.Offsets) > 10000 || options.MaxDuration <= 0 || options.MaxDuration > time.Hour || options.MaxFrameBytes < 1 || options.MaxFrameBytes > 64<<20 || options.MaxTotalBytes < int64(options.MaxFrameBytes) || options.MaxTotalBytes > 1<<30 || options.MaxPixels < 1 || options.MaxPixels > 16<<20 {
		return result, errors.New("invalid video sampling bounds")
	}
	for i, offset := range options.Offsets {
		if offset < 0 || offset > options.MaxDuration || i > 0 && offset <= options.Offsets[i-1] {
			return result, errors.New("video offsets must increase within the selected duration")
		}
	}
	var target, space string
	for i, offset := range options.Offsets {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		frame, err := decoder.FrameAt(ctx, offset)
		if err != nil {
			return result, err
		}
		img, err := decode(frame, options.MaxFrameBytes, options.MaxPixels)
		if err != nil {
			return result, err
		}
		if i == 0 {
			target, space = frame.Target, frame.CoordinateSpace
		}
		if frame.Target != target || frame.CoordinateSpace != space {
			return result, errors.New("video target or coordinate space changed")
		}
		if result.Bytes+int64(len(frame.Data)) > options.MaxTotalBytes {
			return result, errors.New("video sampling exceeds total byte ceiling")
		}
		result.Bytes += int64(len(frame.Data))
		digest := sha256.Sum256(frame.Data)
		meta := Metadata{Sequence: i, ObservedAt: frame.ObservedAt, Target: frame.Target, CoordinateSpace: frame.CoordinateSpace, Width: img.Bounds().Dx(), Height: img.Bounds().Dy(), Bytes: len(frame.Data), SHA256: hex.EncodeToString(digest[:])}
		ref, err := consume(ctx, frame, meta)
		if err != nil {
			return result, err
		}
		meta.Reference = ref
		result.Frames = append(result.Frames, VideoFrame{OffsetMS: offset.Milliseconds(), Metadata: meta})
	}
	result.Complete = true
	return result, nil
}
