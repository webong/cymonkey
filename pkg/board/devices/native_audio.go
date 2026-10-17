package devices

import (
	"devicekit"
	"devicekit/device"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// NativeAudioOptions selects a trusted local helper and fixed host-owned sources.
// Remote actions cannot choose executable paths, arguments, or device IDs.
type NativeAudioOptions struct {
	ID, Name, Helper                               string
	SystemSource, MicrophoneSource, PlaybackSource string
	SystemCapture, Microphone, Playback            bool
}

func NativeAudio(o NativeAudioOptions) (*device.Provider, error) {
	if o.ID == "" || !filepath.IsAbs(o.Helper) {
		return nil, errors.New("native audio requires an ID and absolute helper path")
	}
	if _, err := exec.LookPath(o.Helper); err != nil {
		return nil, errors.New("native audio helper unavailable")
	}
	converter, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.New("native audio requires ffmpeg format conversion")
	}
	caps := []devicekit.CapabilityDescriptor{device.Descriptor(devicekit.AudioDescribe, device.EmptySchema, false)}
	enabled := map[devicekit.Capability]bool{devicekit.AudioCapture: o.SystemCapture, devicekit.AudioMicrophone: o.Microphone, devicekit.AudioPlayback: o.Playback}
	for cap, on := range enabled {
		if on {
			caps = append(caps, device.Descriptor(cap, `{"type":"object","properties":{"encoding":{"enum":["pcm","pcmu"]}},"additionalProperties":false}`, false))
		}
	}
	return &device.Provider{ProviderID: o.ID, Device: devicekit.Device{ID: "audio", Kind: "audio", Name: o.Name, Capabilities: caps}, Bind: func(context.Context, devicekit.Grant) (device.Binding, error) {
		ops := map[devicekit.Capability]device.Operation{}
		ops[devicekit.AudioDescribe] = device.Operation{Invoke: func(ctx context.Context, a devicekit.Action) (devicekit.Result, error) {
			if err := device.Empty(a.Input); err != nil {
				return devicekit.Result{}, err
			}
			probe, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			raw, err := (commandRunner{}).Run(probe, o.Helper, "describe", "")
			if err != nil {
				return devicekit.Result{}, err
			}
			var availability map[string]any
			if err := json.Unmarshal(raw, &availability); err != nil {
				return devicekit.Result{}, errors.New("invalid native audio readiness response")
			}
			return device.Result(map[string]any{"backend": "native-helper", "systemSource": o.SystemSource, "microphoneSource": o.MicrophoneSource, "playbackSource": o.PlaybackSource, "encodings": []string{"pcm", "pcmu"}, "availability": availability})
		}}
		for cap, on := range enabled {
			if !on {
				continue
			}
			ops[cap] = device.Operation{Channel: func(ctx context.Context, a devicekit.Action, l devicekit.ChannelLimits) (io.ReadWriteCloser, error) {
				var in struct {
					Encoding string `json:"encoding"`
				}
				if err := device.Decode(a.Input, &in); err != nil {
					return nil, err
				}
				format, rate, channels := "s16le", "48000", "2"
				if in.Encoding == "pcmu" {
					format, rate, channels = "mulaw", "8000", "1"
				} else if in.Encoding != "" && in.Encoding != "pcm" {
					return nil, errors.New("unsupported native audio encoding")
				}
				mode, source := "capture-system", o.SystemSource
				read := cap != devicekit.AudioPlayback
				if cap == devicekit.AudioMicrophone {
					mode, source = "capture-microphone", o.MicrophoneSource
				}
				if !read {
					mode, source = "playback", o.PlaybackSource
				}
				if read && l.WriteBytes != 0 || !read && l.ReadBytes != 0 {
					return nil, devicekit.ErrNotGranted
				}
				args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
				if read {
					args = append(args, "-probesize", "32768", "-analyzeduration", "0", "-f", "wav", "-i", "pipe:0", "-f", format, "-ar", rate, "-ac", channels, "pipe:1")
				} else {
					args = append(args, "-f", format, "-ar", rate, "-ac", channels, "-i", "pipe:0", "-f", "s16le", "-ar", "48000", "-ac", "2", "pipe:1")
				}
				return nativeAudioPipeline(ctx, o.Helper, []string{mode, source}, converter, args, read)
			}}
		}
		return device.Binding{Operations: ops}, nil
	}}, nil
}

type nativeAudioChannel struct {
	reader *os.File
	writer *os.File
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (c *nativeAudioChannel) Read(b []byte) (int, error) {
	if c.reader == nil {
		return 0, devicekit.ErrNotGranted
	}
	return c.reader.Read(b)
}
func (c *nativeAudioChannel) Write(b []byte) (int, error) {
	if c.writer == nil {
		return 0, devicekit.ErrNotGranted
	}
	return c.writer.Write(b)
}
func (c *nativeAudioChannel) Close() error {
	c.once.Do(func() {
		c.cancel()
		if c.reader != nil {
			c.reader.Close()
		}
		if c.writer != nil {
			c.writer.Close()
		}
		<-c.done
	})
	return nil
}

// OS pipes bound memory and apply backpressure. Both processes belong to this
// channel. Explicit pipe ownership keeps Wait from truncating unread output.
func nativeAudioPipeline(ctx context.Context, helper string, helperArgs []string, converter string, args []string, read bool) (io.ReadWriteCloser, error) {
	life, cancel := context.WithCancel(ctx)
	bridgeR, bridgeW, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, err
	}
	outerR, outerW, err := os.Pipe()
	if err != nil {
		cancel()
		bridgeR.Close()
		bridgeW.Close()
		return nil, err
	}
	native := exec.CommandContext(life, helper, helperArgs...)
	convert := exec.CommandContext(life, converter, args...)
	c := &nativeAudioChannel{cancel: cancel, done: make(chan struct{})}
	if read {
		native.Stdout = bridgeW
		convert.Stdin = bridgeR
		convert.Stdout = outerW
		c.reader = outerR
	} else {
		convert.Stdin = outerR
		convert.Stdout = bridgeW
		native.Stdin = bridgeR
		c.writer = outerW
	}
	if err = native.Start(); err == nil {
		err = convert.Start()
		if err != nil {
			cancel()
			_ = native.Wait()
		}
	}
	bridgeR.Close()
	bridgeW.Close()
	if err != nil {
		cancel()
		outerR.Close()
		outerW.Close()
		return nil, errors.New("native audio pipeline could not start")
	}
	if read {
		outerW.Close()
	} else {
		outerR.Close()
	}
	go func() {
		var wg sync.WaitGroup
		wg.Add(2)
		for _, cmd := range []*exec.Cmd{native, convert} {
			go func() { defer wg.Done(); _ = cmd.Wait(); cancel() }()
		}
		wg.Wait()
		close(c.done)
	}()
	return c, nil
}
