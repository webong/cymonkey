package devices

import (
	"devicekit"
	"devicekit/device"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// NativePointer binds pointer operations to one approved display. A single
// session owns this provider at a time; its helper releases holds on EOF or idle.
func NativePointer(id, name, helper, source string) (*device.Provider, error) {
	if id == "" || !filepath.IsAbs(helper) || source == "" {
		return nil, errors.New("pointer needs an ID, absolute helper and approved display")
	}
	if _, err := exec.LookPath(helper); err != nil {
		return nil, err
	}
	var owner sync.Mutex
	caps := []devicekit.CapabilityDescriptor{}
	for _, cap := range []devicekit.Capability{devicekit.PointerMove, devicekit.PointerClick, devicekit.PointerScroll, devicekit.PointerDown, devicekit.PointerUp, devicekit.PointerRelease} {
		if cap == devicekit.PointerRelease {
			caps = append(caps, device.Descriptor(cap, device.EmptySchema, false))
			continue
		}
		caps = append(caps, device.Descriptor(cap, `{"type":"object","properties":{"x":{"type":"number","minimum":0,"maximum":1},"y":{"type":"number","minimum":0,"maximum":1},"button":{"enum":["left","right","middle"]},"deltaX":{"type":"integer","minimum":-100,"maximum":100},"deltaY":{"type":"integer","minimum":-100,"maximum":100}},"required":["x","y"],"additionalProperties":false}`, false))
	}
	return &device.Provider{ProviderID: id, Device: devicekit.Device{ID: "pointer", Kind: "pointer", Name: name, Capabilities: caps}, Bind: func(life context.Context, grant devicekit.Grant) (device.Binding, error) {
		allowed := map[devicekit.Capability]bool{}
		for _, cap := range grant.Capabilities {
			allowed[cap] = true
		}
		if allowed[devicekit.PointerDown] && (!allowed[devicekit.PointerUp] || !allowed[devicekit.PointerRelease] || !allowed[devicekit.PointerMove]) {
			return device.Binding{}, errors.New("held input requires pointer.move, pointer.up and pointer.release grants")
		}
		if !owner.TryLock() {
			return device.Binding{}, errors.New("pointer already owned by another session")
		}
		var mu sync.Mutex
		var process *pointerProcess
		closed := false
		closeSession := func(context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			if closed {
				return nil
			}
			closed = true
			defer owner.Unlock()
			if process != nil {
				return process.Close()
			}
			return nil
		}

		ops := map[devicekit.Capability]device.Operation{}
		for _, cap := range []devicekit.Capability{devicekit.PointerMove, devicekit.PointerClick, devicekit.PointerScroll, devicekit.PointerDown, devicekit.PointerUp, devicekit.PointerRelease} {
			ops[cap] = device.Operation{Invoke: func(ctx context.Context, a devicekit.Action) (result devicekit.Result, invokeErr error) {
				mu.Lock()
				defer mu.Unlock()
				defer func() {
					if invokeErr != nil && process != nil {
						invokeErr = errors.Join(invokeErr, process.Close())
					}
				}()
				if closed || life.Err() != nil {
					return devicekit.Result{}, devicekit.ErrClosed
				}
				if cap == devicekit.PointerRelease {
					if err := device.Empty(a.Input); err != nil {
						return devicekit.Result{}, err
					}
					if process != nil {
						call, cancel := context.WithTimeout(ctx, 3*time.Second)
						defer cancel()
						if err := process.invoke(call, []byte(`{"operation":"pointer.release","x":0,"y":0,"button":"","deltaX":0,"deltaY":0}`)); err != nil {
							return devicekit.Result{}, err
						}
					}
					return device.Result(map[string]bool{"submitted": true})
				}
				var in struct {
					X      *float64 `json:"x"`
					Y      *float64 `json:"y"`
					Button string   `json:"button,omitempty"`
					DeltaX int      `json:"deltaX,omitempty"`
					DeltaY int      `json:"deltaY,omitempty"`
				}
				if err := device.Decode(a.Input, &in); err != nil {
					return devicekit.Result{}, err
				}
				if in.X == nil || in.Y == nil || math.IsNaN(*in.X) || math.IsNaN(*in.Y) || *in.X < 0 || *in.X > 1 || *in.Y < 0 || *in.Y > 1 || in.DeltaX < -100 || in.DeltaX > 100 || in.DeltaY < -100 || in.DeltaY > 100 {
					return devicekit.Result{}, errors.New("invalid pointer coordinates or delta")
				}
				switch cap {
				case devicekit.PointerClick, devicekit.PointerDown, devicekit.PointerUp:
					if in.Button != "left" && in.Button != "right" && in.Button != "middle" {
						return devicekit.Result{}, errors.New("explicit pointer button required")
					}
					if in.DeltaX != 0 || in.DeltaY != 0 {
						return devicekit.Result{}, errors.New("click cannot scroll")
					}
				case devicekit.PointerMove:
					if in.Button != "" || in.DeltaX != 0 || in.DeltaY != 0 {
						return devicekit.Result{}, errors.New("move cannot click or scroll")
					}
				case devicekit.PointerScroll:
					if in.Button != "" {
						return devicekit.Result{}, errors.New("scroll cannot click")
					}
				}
				raw, err := json.Marshal(map[string]any{"operation": cap, "x": *in.X, "y": *in.Y, "button": in.Button, "deltaX": in.DeltaX, "deltaY": in.DeltaY})
				if err != nil {
					return devicekit.Result{}, err
				}
				call, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				if process == nil && cap == devicekit.PointerDown {
					process, err = startPointerProcess(helper, source)
					if err != nil {
						return devicekit.Result{}, err
					}
				}
				if process != nil {
					err = process.invoke(call, raw)
				} else if cap != devicekit.PointerUp {
					_, err = (commandRunner{}).Run(call, helper, "pointer", source, string(raw))
				}
				if err != nil {
					return devicekit.Result{}, err
				}
				return device.Result(map[string]bool{"submitted": true})
			}}
		}
		context.AfterFunc(life, func() { _ = closeSession(context.Background()) })
		return device.Binding{Operations: ops, Close: closeSession}, nil
	}}, nil
}

// The helper owns the native button state. EOF, malformed input and a five-second
// held-input idle timeout release it even when the parent exits unexpectedly.
type pointerProcess struct {
	cmd     *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	reader  *bufio.Reader
	done    chan struct{}
	once    sync.Once
	err     error
	waitErr error
}

func startPointerProcess(helper, source string) (*pointerProcess, error) {
	cmd := exec.Command(helper, "pointer-session", source)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	p := &pointerProcess{cmd: cmd, input: input, output: output, reader: bufio.NewReaderSize(output, 128), done: make(chan struct{})}
	go func() { p.waitErr = cmd.Wait(); close(p.done) }()
	return p, nil
}
func (p *pointerProcess) invoke(ctx context.Context, raw []byte) error {
	if err := ctx.Err(); err != nil {
		_ = p.Close()
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = p.Close() })
	defer stop()
	if _, err := p.input.Write(append(raw, '\n')); err != nil {
		_ = p.Close()
		return err
	}
	line, err := p.reader.ReadSlice('\n')
	if err != nil || (string(line) != "ok\n" && string(line) != "ok\r\n") {
		_ = p.Close()
		return errors.New("pointer helper stopped or rejected input")
	}
	return ctx.Err()
}
func (p *pointerProcess) Close() error {
	p.once.Do(func() {
		_ = p.input.Close() // EOF asks helper to release its owned button before exiting.
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			p.err = errors.New("pointer helper did not complete release before shutdown")
			_ = p.cmd.Process.Kill()
			<-p.done
		}
		p.err = errors.Join(p.err, p.waitErr)
		_ = p.output.Close()
	})
	return p.err
}
