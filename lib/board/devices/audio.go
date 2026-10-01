package devices

import (
	"board"
	"board/audio"
	"board/device"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
)

type AudioRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type AudioOptions struct {
	ID, Name, Socket, OutputDevice, InputDevice    string
	Playback, Microphone, SystemCapture, Configure bool
	Runner                                         AudioRunner
	LookPath                                       func(string) (string, error)
}

var audioName = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

func Audio(o AudioOptions) (*device.Provider, error) {
	if o.ID == "" || !filepath.IsAbs(o.Socket) {
		return nil, errors.New("audio needs a provider ID and absolute private PulseAudio socket")
	}
	if o.OutputDevice == "" {
		o.OutputDevice = "board_output"
	}
	if o.InputDevice == "" {
		o.InputDevice = "board_microphone"
	}
	if !audioName.MatchString(o.OutputDevice) || !audioName.MatchString(o.InputDevice) {
		return nil, errors.New("invalid audio device name")
	}
	if o.Runner == nil {
		o.Runner = commandRunner{}
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	for _, name := range []string{"pactl", "pacat"} {
		if _, err := o.LookPath(name); err != nil {
			return nil, errors.New("required audio tool unavailable: " + name)
		}
	}
	server := "--server=unix:" + o.Socket
	caps := []board.CapabilityDescriptor{device.Descriptor(board.AudioDescribe, device.EmptySchema, false), device.Descriptor(board.DeviceHealth, device.EmptySchema, false)}
	schema := `{"type":"object","properties":{"encoding":{"enum":["pcm","pcmu"]},"sampleRate":{"type":"integer","minimum":8000,"maximum":192000},"channels":{"type":"integer","minimum":1,"maximum":8}},"additionalProperties":false}`
	for cap, enabled := range map[board.Capability]bool{board.AudioCapture: o.SystemCapture, board.AudioMicrophone: o.Microphone, board.AudioPlayback: o.Playback} {
		if enabled {
			caps = append(caps, device.Descriptor(cap, schema, false))
		}
	}
	if o.Configure {
		caps = append(caps, device.Descriptor(board.AudioConfigure, `{"type":"object","properties":{"direction":{"enum":["input","output"]},"level":{"type":"integer","minimum":0,"maximum":100},"muted":{"type":"boolean"}},"required":["direction","level","muted"],"additionalProperties":false}`, false))
	}
	return &device.Provider{ProviderID: o.ID, Device: board.Device{ID: "audio", Kind: "audio", Name: o.Name, Capabilities: caps}, Bind: func(context.Context, board.Grant) (device.Binding, error) {
		ops := map[board.Capability]device.Operation{}
		inspect := func(ctx context.Context, a board.Action) (board.Result, error) {
			if err := device.Empty(a.Input); err != nil {
				return board.Result{}, err
			}
			sinks, err := o.Runner.Run(ctx, "pactl", server, "list", "short", "sinks")
			if err != nil {
				return board.Result{}, err
			}
			sources, err := o.Runner.Run(ctx, "pactl", server, "list", "short", "sources")
			if err != nil {
				return board.Result{}, err
			}
			result := audio.ParsePulseDevices(string(sinks), string(sources), audio.Request{Playback: o.Playback, Microphone: o.Microphone, SystemCapture: o.SystemCapture, OutputDevice: o.OutputDevice, InputDevice: o.InputDevice})
			selected := result.Devices[:0]
			for _, d := range result.Devices {
				if d.ID == o.OutputDevice || d.ID == o.InputDevice || d.ID == o.OutputDevice+".monitor" {
					selected = append(selected, d)
				}
			}
			result.Devices = selected
			return device.Result(result)
		}
		ops[board.AudioDescribe] = device.Operation{Invoke: inspect}
		ops[board.DeviceHealth] = device.Operation{Invoke: inspect}
		for _, cap := range []board.Capability{board.AudioCapture, board.AudioMicrophone, board.AudioPlayback} {
			cap := cap
			ops[cap] = device.Operation{Channel: func(ctx context.Context, a board.Action, l board.ChannelLimits) (io.ReadWriteCloser, error) {
				var in struct {
					Encoding   string `json:"encoding"`
					SampleRate *int   `json:"sampleRate"`
					Channels   *int   `json:"channels"`
				}
				if err := device.Decode(a.Input, &in); err != nil {
					return nil, err
				}
				format, sampleRate, channelCount := "s16le", 48000, 2
				switch in.Encoding {
				case "", "pcm":
				case "pcmu":
					format, sampleRate, channelCount = "ulaw", 8000, 1
				default:
					return nil, errors.New("unsupported audio encoding")
				}
				if in.SampleRate != nil {
					if *in.SampleRate < 8000 || *in.SampleRate > 192000 || in.Encoding == "pcmu" && *in.SampleRate != 8000 {
						return nil, errors.New("invalid audio sample rate")
					}
					sampleRate = *in.SampleRate
				}
				if in.Channels != nil {
					if *in.Channels < 1 || *in.Channels > 8 || in.Encoding == "pcmu" && *in.Channels != 1 {
						return nil, errors.New("invalid audio channel count")
					}
					channelCount = *in.Channels
				}
				read := cap != board.AudioPlayback
				if read && l.WriteBytes != 0 || !read && l.ReadBytes != 0 {
					return nil, board.ErrNotGranted
				}
				direction, target := "--record", o.OutputDevice+".monitor"
				if cap == board.AudioMicrophone {
					target = o.InputDevice
				}
				if !read {
					direction, target = "--playback", o.OutputDevice
				}
				return processStream(ctx, nil, read, "pacat", server, direction, "--raw", "--device="+target, "--format="+format, "--rate="+strconv.Itoa(sampleRate), "--channels="+strconv.Itoa(channelCount))
			}}
		}
		ops[board.AudioConfigure] = device.Operation{Invoke: func(ctx context.Context, a board.Action) (board.Result, error) {
			var in struct {
				Direction string `json:"direction"`
				Level     *int   `json:"level"`
				Muted     *bool  `json:"muted"`
			}
			if err := device.Decode(a.Input, &in); err != nil {
				return board.Result{}, err
			}
			if in.Level == nil || in.Muted == nil || *in.Level < 0 || *in.Level > 100 {
				return board.Result{}, errors.New("audio level and muted are required")
			}
			kind, target := "sink", o.OutputDevice
			if in.Direction == "input" {
				kind, target = "source", o.InputDevice
			} else if in.Direction != "output" {
				return board.Result{}, errors.New("invalid audio direction")
			}
			if _, err := o.Runner.Run(ctx, "pactl", server, "set-"+kind+"-volume", target, strconv.Itoa(*in.Level)+"%"); err != nil {
				return board.Result{}, err
			}
			if _, err := o.Runner.Run(ctx, "pactl", server, "set-"+kind+"-mute", target, strconv.FormatBool(*in.Muted)); err != nil {
				return board.Result{}, err
			}
			return device.Result(map[string]bool{"applied": true})
		}}
		return device.Binding{Operations: ops, Health: func(ctx context.Context) (bool, error) {
			v, err := inspect(ctx, board.Action{})
			if err != nil {
				return false, err
			}
			var observation audio.Observation
			err = json.Unmarshal(v.Output, &observation)
			return observation.Ready, err
		}}, nil
	}}, nil
}
