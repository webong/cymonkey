// Package clipboard exposes bounded clipboard access through explicit Board
// grants. The clipboard model, limits and checks are ext's
// res/system/capture/clipboard; native implementations belong to
// caller-selected runtime packages.
package clipboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"jangolova/devicekit"
	"jangolova/devicekit/device"

	sysclipboard "github.com/webong/ext/res/system/capture/clipboard"
)

const (
	Describe devicekit.Capability = "clipboard.describe"
	Read     devicekit.Capability = "clipboard.read"
	Write    devicekit.Capability = "clipboard.write"
	Clear    devicekit.Capability = "clipboard.clear"
)

// ErrChanged is ext's compare-and-swap failure: the generation moved on.
var ErrChanged = sysclipboard.ErrChanged

type (
	Representation = sysclipboard.Representation
	Item           = sysclipboard.Item
	Snapshot       = sysclipboard.Snapshot
	WriteRequest   = sysclipboard.WriteRequest
	Receipt        = sysclipboard.Receipt
	// Backend must serialize operations and compare ExpectedGeneration
	// directly before changing the native clipboard. Restoration is another
	// explicit write.
	Backend = sysclipboard.Backend
)

type Options struct {
	ProviderID string
	DeviceID   string
	ResourceID string
	MaxBytes   int64
	Formats    []string
}

// NewProvider borrows a backend. Closing a Board attachment never clears,
// restores or closes the system clipboard. Every write supplies the observed
// generation; atomicity against other processes depends on the native backend.
// Board grants decide which capabilities a session may call, so the ext
// clipboard policy allows both reads and writes.
func NewProvider(backend Backend, options Options) (*device.Provider, error) {
	if options.ProviderID == "" || options.DeviceID == "" || options.ResourceID == "" {
		return nil, errors.New("clipboard needs a backend, IDs, byte ceiling and allowed formats")
	}
	board, err := sysclipboard.New(backend, sysclipboard.Policy{MaxBytes: options.MaxBytes, AllowedFormats: options.Formats, AllowRead: true, AllowWrite: true})
	if err != nil {
		return nil, fmt.Errorf("clipboard needs a backend, IDs, byte ceiling and allowed formats: %w", err)
	}
	return &device.Provider{ProviderID: options.ProviderID, Device: devicekit.Device{ID: options.DeviceID, Kind: "clipboard", Capabilities: []devicekit.CapabilityDescriptor{
		device.Descriptor(Describe, device.EmptySchema, true), device.Descriptor(Read, device.EmptySchema, true),
		device.Descriptor(Write, `{"type":"object","required":["expectedGeneration","items"],"properties":{"expectedGeneration":{"type":"string"},"items":{"type":"array"}},"additionalProperties":false}`, true),
		device.Descriptor(Clear, `{"type":"object","required":["expectedGeneration"],"properties":{"expectedGeneration":{"type":"string"}},"additionalProperties":false}`, true),
	}}, ResourceIDs: []string{options.ResourceID}, Bind: func(_ context.Context, _ devicekit.Grant) (device.Binding, error) {
		read := func(ctx context.Context, a devicekit.Action) (Snapshot, error) {
			if err := device.Empty(a.Input); err != nil {
				return Snapshot{}, err
			}
			return board.Read(ctx)
		}
		write := func(ctx context.Context, a devicekit.Action) (devicekit.Result, error) {
			var receipt Receipt
			if a.Capability == Clear {
				var c struct {
					ExpectedGeneration string `json:"expectedGeneration"`
				}
				if err := device.Decode(a.Input, &c); err != nil {
					return devicekit.Result{}, err
				}
				r, err := board.Clear(ctx, c.ExpectedGeneration)
				if err != nil {
					return devicekit.Result{}, err
				}
				receipt = r
			} else {
				var request WriteRequest
				if err := device.Decode(a.Input, &request); err != nil {
					return devicekit.Result{}, err
				}
				r, err := board.Write(ctx, request)
				if err != nil {
					return devicekit.Result{}, err
				}
				receipt = r
			}
			return device.Result(receipt)
		}
		return device.Binding{Operations: map[devicekit.Capability]device.Operation{
			Describe: {Invoke: func(ctx context.Context, a devicekit.Action) (devicekit.Result, error) {
				s, err := read(ctx, a)
				if err != nil {
					return devicekit.Result{}, err
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
			Read: {Stream: func(ctx context.Context, a devicekit.Action) (devicekit.Content, error) {
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
// The native helper protocol has no generation-only call, so Generation is
// unsupported; Board never calls it.
type Caller interface {
	Call(context.Context, string, json.RawMessage) (json.RawMessage, error)
}
type Bridge struct {
	Caller     Caller
	ResourceID string
}

var _ Backend = Bridge{}

func (b Bridge) call(ctx context.Context, name string, input map[string]any, out any) error {
	if b.Caller == nil || b.ResourceID == "" {
		return errors.New("clipboard bridge is unconfigured")
	}
	limit, ok := input["maxBytes"].(int64)
	if !ok || limit <= 0 || limit > sysclipboard.MaxPolicyBytes {
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

func (Bridge) Generation(context.Context) (string, error) {
	return "", errors.New("the native clipboard bridge has no generation-only call")
}

func (b Bridge) Read(ctx context.Context, limit int64, _ []string) (Snapshot, error) {
	var s Snapshot
	err := b.call(ctx, string(Read), map[string]any{"maxBytes": limit}, &s)
	return s, err
}

func (b Bridge) Write(ctx context.Context, r WriteRequest, limit int64, _ []string) (Receipt, error) {
	var out Receipt
	err := b.call(ctx, string(Write), map[string]any{"maxBytes": limit, "expectedGeneration": r.ExpectedGeneration, "items": r.Items}, &out)
	return out, err
}
