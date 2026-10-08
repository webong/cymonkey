package devices

import (
	"board"
	"board/device"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os/exec"
	"path/filepath"
	"time"
)

// NativePointer binds atomic pointer operations to one host-approved display.
// No held buttons survive a failed request or disconnected session.
func NativePointer(id, name, helper, source string) (*device.Provider, error) {
	if id == "" || !filepath.IsAbs(helper) || source == "" {
		return nil, errors.New("pointer needs an ID, absolute helper and approved display")
	}
	if _, err := exec.LookPath(helper); err != nil {
		return nil, err
	}
	caps := []board.CapabilityDescriptor{}
	for _, cap := range []board.Capability{board.PointerMove, board.PointerClick, board.PointerScroll} {
		caps = append(caps, device.Descriptor(cap, `{"type":"object","properties":{"x":{"type":"number","minimum":0,"maximum":1},"y":{"type":"number","minimum":0,"maximum":1},"button":{"enum":["left","right","middle"]},"deltaX":{"type":"integer","minimum":-100,"maximum":100},"deltaY":{"type":"integer","minimum":-100,"maximum":100}},"required":["x","y"],"additionalProperties":false}`, false))
	}
	return &device.Provider{ProviderID: id, Device: board.Device{ID: "pointer", Kind: "pointer", Name: name, Capabilities: caps}, Bind: func(context.Context, board.Grant) (device.Binding, error) {
		ops := map[board.Capability]device.Operation{}
		for _, cap := range []board.Capability{board.PointerMove, board.PointerClick, board.PointerScroll} {
			ops[cap] = device.Operation{Invoke: func(ctx context.Context, a board.Action) (board.Result, error) {
				var in struct {
					X      *float64 `json:"x"`
					Y      *float64 `json:"y"`
					Button string   `json:"button,omitempty"`
					DeltaX int      `json:"deltaX,omitempty"`
					DeltaY int      `json:"deltaY,omitempty"`
				}
				if err := device.Decode(a.Input, &in); err != nil {
					return board.Result{}, err
				}
				if in.X == nil || in.Y == nil || math.IsNaN(*in.X) || math.IsNaN(*in.Y) || *in.X < 0 || *in.X > 1 || *in.Y < 0 || *in.Y > 1 || in.DeltaX < -100 || in.DeltaX > 100 || in.DeltaY < -100 || in.DeltaY > 100 {
					return board.Result{}, errors.New("invalid pointer coordinates or delta")
				}
				switch cap {
				case board.PointerClick:
					if in.Button != "left" && in.Button != "right" && in.Button != "middle" {
						return board.Result{}, errors.New("explicit pointer button required")
					}
					if in.DeltaX != 0 || in.DeltaY != 0 {
						return board.Result{}, errors.New("click cannot scroll")
					}
				case board.PointerMove:
					if in.Button != "" || in.DeltaX != 0 || in.DeltaY != 0 {
						return board.Result{}, errors.New("move cannot click or scroll")
					}
				case board.PointerScroll:
					if in.Button != "" {
						return board.Result{}, errors.New("scroll cannot click")
					}
				}
				raw, _ := json.Marshal(map[string]any{"operation": cap, "x": *in.X, "y": *in.Y, "button": in.Button, "deltaX": in.DeltaX, "deltaY": in.DeltaY})
				life, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				if _, err := (commandRunner{}).Run(life, helper, "pointer", source, string(raw)); err != nil {
					return board.Result{}, err
				}
				return device.Result(map[string]bool{"submitted": true})
			}}
		}
		return device.Binding{Operations: ops}, nil
	}}, nil
}
