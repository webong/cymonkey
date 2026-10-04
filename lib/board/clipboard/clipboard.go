// Package clipboard exposes bounded clipboard access through explicit Board
// grants. Native implementations belong to caller-selected runtime packages.
package clipboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"board"
	"board/device"
)

const (
	Describe board.Capability = "clipboard.describe"
	Read     board.Capability = "clipboard.read"
	Write    board.Capability = "clipboard.write"
	Clear    board.Capability = "clipboard.clear"
)

var ErrChanged = errors.New("clipboard generation changed")

type Representation struct {
	Format string `json:"format"`
	Data   []byte `json:"data"`
}
type Item struct {
	Representations []Representation `json:"representations"`
}
type Snapshot struct {
	Generation string `json:"generation"`
	Items      []Item `json:"items"`
}
type WriteRequest struct {
	ExpectedGeneration string `json:"expectedGeneration"`
	Items              []Item `json:"items"`
}
type Receipt struct {
	Generation string `json:"generation"`
	Dispatch   string `json:"dispatch"`
}

// Backend must serialize operations and compare ExpectedGeneration directly
// before changing the native clipboard. Restoration is another explicit write.
type Backend interface {
	Read(context.Context, int64) (Snapshot, error)
	Write(context.Context, WriteRequest, int64) (Receipt, error)
}
type Options struct {
	ProviderID string
	DeviceID   string
	ResourceID string
	MaxBytes   int64
	Formats    []string
}

func Validate(snapshot Snapshot, maxBytes int64, formats []string) error {
	if snapshot.Generation == "" || maxBytes <= 0 || maxBytes > 16<<20 || len(snapshot.Items) > 64 {
		return errors.New("invalid clipboard snapshot or bounds")
	}
	allowed := map[string]bool{}
	for _, format := range formats {
		allowed[format] = true
	}
	var size int64
	for _, item := range snapshot.Items {
		if len(item.Representations) == 0 || len(item.Representations) > 32 {
			return errors.New("invalid clipboard representations")
		}
		seen := map[string]bool{}
		for _, r := range item.Representations {
			if r.Format == "" || seen[r.Format] || !allowed[r.Format] {
				return errors.New("clipboard format is not granted or is duplicated")
			}
			seen[r.Format] = true
			size += int64(len(r.Data))
			if size > maxBytes {
				return errors.New("clipboard exceeds byte limit")
			}
		}
	}
	return nil
}

// NewProvider borrows a backend. Closing a Board attachment never clears,
// restores or closes the system clipboard. Every write supplies the observed
// generation; atomicity against other processes depends on the native backend.
func NewProvider(backend Backend, options Options) (*device.Provider, error) {
	if backend == nil || options.ProviderID == "" || options.DeviceID == "" || options.ResourceID == "" || options.MaxBytes <= 0 || options.MaxBytes > 16<<20 || len(options.Formats) == 0 || len(options.Formats) > 32 {
		return nil, errors.New("clipboard needs a backend, IDs, byte ceiling and allowed formats")
	}
	seen := map[string]bool{}
	for _, format := range options.Formats {
		if format == "" || len(format) > 256 || seen[format] {
			return nil, errors.New("invalid or duplicate clipboard format")
		}
		seen[format] = true
	}
	options.Formats = append([]string(nil), options.Formats...)
	return &device.Provider{ProviderID: options.ProviderID, Device: board.Device{ID: options.DeviceID, Kind: "clipboard", Capabilities: []board.CapabilityDescriptor{
		device.Descriptor(Describe, device.EmptySchema, true), device.Descriptor(Read, device.EmptySchema, true),
		device.Descriptor(Write, `{"type":"object","required":["expectedGeneration","items"],"properties":{"expectedGeneration":{"type":"string"},"items":{"type":"array"}},"additionalProperties":false}`, true),
		device.Descriptor(Clear, `{"type":"object","required":["expectedGeneration"],"properties":{"expectedGeneration":{"type":"string"}},"additionalProperties":false}`, true),
	}}, ResourceIDs: []string{options.ResourceID}, Bind: func(_ context.Context, _ board.Grant) (device.Binding, error) {
		read := func(ctx context.Context, a board.Action) (Snapshot, error) {
			if err := device.Empty(a.Input); err != nil {
				return Snapshot{}, err
			}
			s, err := backend.Read(ctx, options.MaxBytes)
			if err != nil {
				return s, err
			}
			return s, Validate(s, options.MaxBytes, options.Formats)
		}
		write := func(ctx context.Context, a board.Action) (board.Result, error) {
			var r WriteRequest
			if a.Capability == Clear {
				var c struct {
					ExpectedGeneration string `json:"expectedGeneration"`
				}
				if err := device.Decode(a.Input, &c); err != nil {
					return board.Result{}, err
				}
				r.ExpectedGeneration = c.ExpectedGeneration
				r.Items = []Item{}
			} else if err := device.Decode(a.Input, &r); err != nil {
				return board.Result{}, err
			}
			if err := Validate(Snapshot{Generation: r.ExpectedGeneration, Items: r.Items}, options.MaxBytes, options.Formats); err != nil {
				return board.Result{}, err
			}
			receipt, err := backend.Write(ctx, r, options.MaxBytes)
			if err != nil {
				return board.Result{}, err
			}
			if receipt.Generation == "" || receipt.Dispatch != "dispatched" {
				return board.Result{}, errors.New("clipboard write returned unknown dispatch")
			}
			return device.Result(receipt)
		}
		return device.Binding{Operations: map[board.Capability]device.Operation{
			Describe: {Invoke: func(ctx context.Context, a board.Action) (board.Result, error) {
				s, err := read(ctx, a)
				if err != nil {
					return board.Result{}, err
				}
				type meta struct {
					Format string `json:"format"`
					Bytes  int    `json:"bytes"`
				}
				items := make([][]meta, len(s.Items))
				for i, item := range s.Items {
					for _, r := range item.Representations {
						items[i] = append(items[i], meta{r.Format, len(r.Data)})
					}
				}
				return device.Result(map[string]any{"generation": s.Generation, "items": items})
			}},
			Read: {Stream: func(ctx context.Context, a board.Action) (board.Content, error) {
				s, err := read(ctx, a)
				if err != nil {
					return nil, err
				}
				data, err := json.Marshal(s)
				if err != nil {
					return nil, err
				}
				return device.Bytes(data), nil
			}}, Write: {Invoke: write}, Clear: {Invoke: write},
		}}, nil
	}}, nil
}

// Bridge borrows an authenticated native caller. The resource is fixed
// by the host; the provider passes its selected byte ceiling for each call.
type Caller interface {
	Call(context.Context, string, json.RawMessage) (json.RawMessage, error)
}
type Bridge struct {
	Caller     Caller
	ResourceID string
}

func (b Bridge) call(ctx context.Context, name string, input map[string]any, out any) error {
	if b.Caller == nil || b.ResourceID == "" {
		return errors.New("clipboard bridge is unconfigured")
	}
	limit, ok := input["maxBytes"].(int64)
	if !ok || limit <= 0 || limit > 16<<20 {
		return errors.New("invalid clipboard byte ceiling")
	}
	input["resourceId"] = b.ResourceID
	params, err := json.Marshal(map[string]any{"name": name, "input": input})
	if err != nil {
		return err
	}
	data, err := b.Caller.Call(ctx, "act", params)
	if err != nil {
		return err
	}
	if int64(len(data)) > limit*2+65536 {
		return errors.New("native clipboard response exceeds ceiling")
	}
	if err = json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode native clipboard: %w", err)
	}
	return nil
}
func (b Bridge) Read(ctx context.Context, limit int64) (Snapshot, error) {
	var s Snapshot
	err := b.call(ctx, string(Read), map[string]any{"maxBytes": limit}, &s)
	return s, err
}
func (b Bridge) Write(ctx context.Context, r WriteRequest, limit int64) (Receipt, error) {
	var out Receipt
	err := b.call(ctx, string(Write), map[string]any{"maxBytes": limit, "expectedGeneration": r.ExpectedGeneration, "items": r.Items}, &out)
	return out, err
}
