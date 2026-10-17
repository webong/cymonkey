package devices

import (
	"board"
	"board/device"
	"board/displayaction"
	"board/displayprobe"
	"board/permissionprobe"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// DesktopOptions explicitly authorizes binding the current entire desktop.
// Environment is fixed at construction; callers cannot choose commands or paths
// through action input. Use macoskeyboard for a process-specific keyboard.
type DesktopOptions struct {
	ID, Name, Scope, OS, RecordingDirectory       string
	Environment                                   map[string]string
	Capture, Input, Video, Recording, Permissions bool
	MaxRecordingDuration                          time.Duration
	Runner                                        displayaction.CommandRunner
	LookPath                                      func(string) (string, error)
}

const RecordingRead = board.RecordingRead

func Desktop(o DesktopOptions) (*device.Provider, error) {
	if o.ID == "" || o.Scope != "desktop" {
		return nil, errors.New("desktop provider requires an ID and explicit scope=desktop")
	}
	if o.OS == "" {
		o.OS = runtime.GOOS
	}
	if o.OS != "darwin" && o.OS != "linux" && o.OS != "windows" {
		return nil, errors.New("unsupported desktop platform")
	}
	envMap := map[string]string{}
	for _, key := range []string{"DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "SESSIONNAME", "CI"} {
		envMap[key] = os.Getenv(key)
	}
	for key, value := range o.Environment {
		switch key {
		case "DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "SESSIONNAME":
			envMap[key] = value
		default:
			return nil, errors.New("unsupported desktop environment key")
		}
	}
	env := []string{}
	for k, v := range envMap {
		env = append(env, k+"="+v)
	}
	if o.Runner == nil {
		o.Runner = commandRunner{env: env}
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	need := func(name string) error {
		_, err := o.LookPath(name)
		if err != nil {
			return errors.New("required desktop tool is unavailable: " + name)
		}
		return nil
	}
	if o.Capture {
		name := "screencapture"
		if o.OS == "windows" {
			name = "powershell"
		}
		if o.OS == "linux" {
			name = "import"
			if envMap["WAYLAND_DISPLAY"] != "" {
				name = "grim"
			}
		}
		if err := need(name); err != nil {
			return nil, err
		}
	}
	if o.Input {
		name := "osascript"
		if o.OS == "windows" {
			name = "powershell"
		}
		if o.OS == "linux" {
			name = "xdotool"
			if envMap["WAYLAND_DISPLAY"] != "" {
				name = "wtype"
			}
		}
		if err := need(name); err != nil {
			return nil, err
		}
	}
	if o.Video || o.Recording {
		if o.OS == "linux" && envMap["DISPLAY"] == "" {
			return nil, errors.New("continuous video and recording currently require an X11 DISPLAY on Linux")
		}
		if err := need("ffmpeg"); err != nil {
			return nil, err
		}
	}
	if o.Recording {
		if o.RecordingDirectory == "" {
			return nil, errors.New("recording needs an approved artifact directory")
		}
		info, err := os.Stat(o.RecordingDirectory)
		if err != nil || !info.IsDir() {
			return nil, errors.New("recording directory unavailable")
		}
	}
	if o.MaxRecordingDuration == 0 {
		o.MaxRecordingDuration = time.Minute
	}
	if o.MaxRecordingDuration < 0 || o.MaxRecordingDuration > time.Hour {
		return nil, errors.New("recording duration must be at most one hour")
	}
	actions := displayaction.New(displayaction.Config{OS: o.OS, Runner: o.Runner, LookupEnv: func(key string) string { return envMap[key] }})
	caps := []board.CapabilityDescriptor{device.Descriptor(board.DisplayDescribe, device.EmptySchema, false), device.Descriptor(board.DeviceHealth, device.EmptySchema, false)}
	if o.Capture {
		caps = append(caps, device.Descriptor(board.DisplayCapture, device.EmptySchema, false))
	}
	if o.Video {
		caps = append(caps, device.Descriptor(board.DisplayVideo, device.EmptySchema, false))
	}
	if o.Input {
		caps = append(caps, device.Descriptor(board.KeyboardPress, `{"type":"object","properties":{"key":{"type":"string","maxLength":128}},"required":["key"],"additionalProperties":false}`, false))
		if o.OS != "linux" {
			caps = append(caps, device.Descriptor(board.KeyboardType, `{"type":"object","properties":{"text":{"type":"string","maxLength":1024}},"required":["text"],"additionalProperties":false}`, false))
		}
	}
	if o.Recording {
		caps = append(caps, device.Descriptor(board.RecordingStart, `{"type":"object","properties":{"format":{"enum":["mp4","webm"]}},"required":["format"],"additionalProperties":false}`, false), device.Descriptor(board.RecordingStop, device.EmptySchema, false), device.Descriptor(RecordingRead, device.EmptySchema, false))
	}
	if o.Permissions {
		caps = append(caps, device.Descriptor(board.PermissionOpen, `{"type":"object","properties":{"permission":{"type":"string"}},"required":["permission"],"additionalProperties":false}`, false))
	}
	return &device.Provider{ProviderID: o.ID, Device: board.Device{ID: "desktop", Kind: "display", Name: o.Name, Capabilities: caps}, Bind: func(life context.Context, g board.Grant) (device.Binding, error) {
		state := &recordingState{events: make(chan board.Event, 16)}
		ops := map[board.Capability]device.Operation{}
		describe := func(_ context.Context, a board.Action) (board.Result, error) {
			if err := device.Empty(a.Input); err != nil {
				return board.Result{}, err
			}
			return device.Result(displayprobe.New(displayprobe.Config{OS: o.OS, LookupEnv: func(key string) (string, bool) { v, ok := envMap[key]; return v, ok }, LookPath: o.LookPath}).Observe())
		}
		ops[board.DisplayDescribe] = device.Operation{Invoke: describe}
		ops[board.DeviceHealth] = device.Operation{Invoke: describe}
		ops[board.DisplayCapture] = device.Operation{Stream: func(ctx context.Context, a board.Action) (board.Content, error) {
			if err := device.Empty(a.Input); err != nil {
				return nil, err
			}
			var b []byte
			var err error
			if o.OS == "linux" {
				if envMap["WAYLAND_DISPLAY"] != "" {
					b, err = o.Runner.Run(ctx, "grim", "-")
				} else {
					b, err = o.Runner.Run(ctx, "import", "-window", "root", "png:-")
				}
			} else {
				b, err = actions.Capture(ctx)
			}
			if err != nil {
				return nil, err
			}
			if len(b) > 16<<20 {
				return nil, board.ErrLimit
			}
			return device.Bytes(b), nil
		}}
		ops[board.DisplayVideo] = device.Operation{Channel: func(ctx context.Context, a board.Action, l board.ChannelLimits) (io.ReadWriteCloser, error) {
			if err := device.Empty(a.Input); err != nil {
				return nil, err
			}
			if l.WriteBytes != 0 {
				return nil, board.ErrNotGranted
			}
			args, err := actions.RecordingArgs("-", "webm")
			if err != nil {
				return nil, err
			}
			args = args[:len(args)-1]
			for i := range args {
				if args[i] == "libvpx-vp9" {
					args[i] = "libvpx"
				}
			}
			args = append(args, "-deadline", "realtime", "-cpu-used", "8", "-f", "ivf", "pipe:1")
			return processStream(ctx, env, true, "ffmpeg", args...)
		}}
		for _, cap := range []board.Capability{board.KeyboardPress, board.KeyboardType} {
			cap := cap
			ops[cap] = device.Operation{Invoke: func(ctx context.Context, a board.Action) (board.Result, error) {
				var key, text string
				if cap == board.KeyboardPress {
					var in struct {
						Key string `json:"key"`
					}
					if err := device.Decode(a.Input, &in); err != nil {
						return board.Result{}, err
					}
					key = in.Key
					if key == "" || strings.HasPrefix(key, "-") || len(key) > 128 || strings.ContainsAny(key, "\x00\r\n") {
						return board.Result{}, errors.New("invalid key")
					}
				} else {
					var in struct {
						Text string `json:"text"`
					}
					if err := device.Decode(a.Input, &in); err != nil {
						return board.Result{}, err
					}
					text = in.Text
					if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > 1024 {
						return board.Result{}, errors.New("invalid text")
					}
				}
				kind := "key"
				if cap == board.KeyboardType {
					kind = "text"
				}
				if err := actions.Input(ctx, displayaction.Input{Kind: kind, Key: key, Text: text}); err != nil {
					return board.Result{}, err
				}
				return device.Result(map[string]bool{"submitted": true})
			}}
		}
		ops[board.PermissionOpen] = device.Operation{Invoke: func(ctx context.Context, a board.Action) (board.Result, error) {
			var in struct {
				Permission string `json:"permission"`
			}
			if err := device.Decode(a.Input, &in); err != nil {
				return board.Result{}, err
			}
			result, err := permissionprobe.New(permissionprobe.Config{OS: o.OS, Runner: o.Runner}).Open(ctx, in.Permission)
			if err != nil {
				return board.Result{}, err
			}
			return device.Result(result)
		}}
		ops[board.RecordingStart] = device.Operation{Invoke: func(_ context.Context, a board.Action) (board.Result, error) {
			var in struct {
				Format string `json:"format"`
			}
			if err := device.Decode(a.Input, &in); err != nil {
				return board.Result{}, err
			}
			if in.Format != "mp4" && in.Format != "webm" {
				return board.Result{}, errors.New("unsupported format")
			}
			state.mu.Lock()
			defer state.mu.Unlock()
			if state.closed || life.Err() != nil {
				return board.Result{}, board.ErrClosed
			}
			if state.path != "" {
				return board.Result{}, errors.New("open a new session for another recording")
			}
			if state.dir == "" {
				var err error
				state.dir, err = os.MkdirTemp(o.RecordingDirectory, "board-recording-")
				if err != nil {
					return board.Result{}, err
				}
			}
			state.path = filepath.Join(state.dir, "capture."+in.Format)
			args, err := actions.RecordingArgs(state.path, in.Format)
			if err != nil {
				return board.Result{}, err
			}
			args = append(args[:len(args)-1], "-fs", "134217728", state.path)
			p, err := o.Runner.Start("ffmpeg", args...)
			if err != nil {
				return board.Result{}, err
			}
			watched := watchCommand(p)
			state.process = watched
			state.timer = time.AfterFunc(o.MaxRecordingDuration, func() { _ = state.stopSpecific(watched) })
			go func() {
				err := watched.Wait()
				state.mu.Lock()
				defer state.mu.Unlock()
				if state.process != watched {
					return
				}
				state.process = nil
				state.timer.Stop()
				kind := "recording.completed"
				if err != nil {
					kind = "recording.failed"
				}
				state.emit(kind)
			}()
			state.emit("recording.started")
			return device.Result(map[string]string{"recordingId": "current", "status": "recording"})
		}}
		ops[board.RecordingStop] = device.Operation{Invoke: func(_ context.Context, a board.Action) (board.Result, error) {
			if err := device.Empty(a.Input); err != nil {
				return board.Result{}, err
			}
			if err := state.stop(); err != nil {
				return board.Result{}, err
			}
			return device.Result(map[string]string{"recordingId": "current", "status": "stopped"})
		}}
		ops[RecordingRead] = device.Operation{Stream: func(_ context.Context, a board.Action) (board.Content, error) {
			if err := device.Empty(a.Input); err != nil {
				return nil, err
			}
			state.mu.Lock()
			defer state.mu.Unlock()
			if state.process != nil || state.path == "" {
				return nil, errors.New("recording must be stopped before reading")
			}
			return openContent(state.path, 128<<20)
		}}
		return device.Binding{Operations: ops, Events: state.events, Health: func(context.Context) (bool, error) {
			v := displayprobe.New(displayprobe.Config{OS: o.OS, LookupEnv: func(key string) (string, bool) { v, ok := envMap[key]; return v, ok }, LookPath: o.LookPath}).Observe()
			return v.Native, nil
		}, Close: func(context.Context) error {
			state.mu.Lock()
			state.closed = true
			state.mu.Unlock()
			err := state.stop()
			state.mu.Lock()
			defer state.mu.Unlock()
			if state.dir != "" {
				err = errors.Join(err, os.RemoveAll(state.dir))
			}
			return err
		}}, nil
	}}, nil
}

type recordingState struct {
	mu        sync.Mutex
	process   displayaction.RunningCommand
	timer     *time.Timer
	path, dir string
	closed    bool
	events    chan board.Event
}

func (s *recordingState) emit(kind string) {
	select {
	case s.events <- board.Event{Type: kind}:
	default:
	}
}
func (s *recordingState) stop() error { return s.stopSpecific(nil) }
func (s *recordingState) stopSpecific(expected displayaction.RunningCommand) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.process == nil || expected != nil && s.process != expected {
		return nil
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := displayaction.StopRecording(ctx, s.process)
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 255 {
		err = nil
	}
	s.process = nil
	kind := "recording.completed"
	if err != nil {
		kind = "recording.failed"
	}
	s.emit(kind)
	return err
}

type fileContent struct {
	*os.File
	reader io.Reader
	mu     sync.Mutex
	size   int64
}

func (f *fileContent) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reader.Read(p)
}
func (f *fileContent) Size() int64   { return f.size }
func (*fileContent) Truncated() bool { return false }
func openContent(path string, limit int64) (board.Content, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > limit {
		_ = f.Close()
		return nil, board.ErrLimit
	}
	return &fileContent{File: f, reader: io.LimitReader(f, st.Size()), size: st.Size()}, nil
}

type watchedCommand struct {
	displayaction.RunningCommand
	done chan struct{}
	err  error
}

func watchCommand(raw displayaction.RunningCommand) *watchedCommand {
	w := &watchedCommand{RunningCommand: raw, done: make(chan struct{})}
	go func() { w.err = raw.Wait(); close(w.done) }()
	return w
}
func (w *watchedCommand) Wait() error { <-w.done; return w.err }
