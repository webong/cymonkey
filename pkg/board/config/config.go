// Package config builds providers from a host-owned, explicit device inventory.
package config

import (
	"board"
	"board/devices"
	"board/rtc"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	pion "github.com/pion/webrtc/v4"
	"io"
	"time"
)

type Config struct {
	Devices []Device `json:"devices"`
}
type Device struct {
	Pointer     *Pointer     `json:"pointer,omitempty"`
	NativeAudio *NativeAudio `json:"nativeAudio,omitempty"`
	ID          string       `json:"id"`
	Name        string       `json:"name,omitempty"`
	Desktop     *Desktop     `json:"desktop,omitempty"`
	Audio       *Audio       `json:"audio,omitempty"`
	Storage     *Storage     `json:"storage,omitempty"`
	Endpoint    *Endpoint    `json:"endpoint,omitempty"`
	WebRTC      *WebRTC      `json:"webrtc,omitempty"`
}
type Pointer struct {
	Helper string `json:"helper"`
	Source string `json:"source"`
}
type NativeAudio struct {
	Helper           string `json:"helper"`
	SystemSource     string `json:"systemSource,omitempty"`
	MicrophoneSource string `json:"microphoneSource,omitempty"`
	PlaybackSource   string `json:"playbackSource,omitempty"`
	SystemCapture    bool   `json:"systemCapture,omitempty"`
	Microphone       bool   `json:"microphone,omitempty"`
	Playback         bool   `json:"playback,omitempty"`
}

type Desktop struct {
	Scope              string            `json:"scope"`
	Environment        map[string]string `json:"environment,omitempty"`
	Capture            bool              `json:"capture,omitempty"`
	Input              bool              `json:"input,omitempty"`
	Video              bool              `json:"video,omitempty"`
	Recording          bool              `json:"recording,omitempty"`
	Permissions        bool              `json:"permissions,omitempty"`
	RecordingDirectory string            `json:"recordingDirectory,omitempty"`
	RecordingSeconds   int               `json:"recordingSeconds,omitempty"`
}
type Audio struct {
	Socket        string `json:"socket"`
	OutputDevice  string `json:"outputDevice"`
	InputDevice   string `json:"inputDevice"`
	Playback      bool   `json:"playback,omitempty"`
	Microphone    bool   `json:"microphone,omitempty"`
	SystemCapture bool   `json:"systemCapture,omitempty"`
	Configure     bool   `json:"configure,omitempty"`
}
type Storage struct {
	Path              string `json:"path"`
	ResourceID        string `json:"resourceId"`
	AllowRestore      bool   `json:"allowRestore,omitempty"`
	MaxTransferBytes  int64  `json:"maxTransferBytes,omitempty"`
	MaxExtractedBytes int64  `json:"maxExtractedBytes,omitempty"`
}
type Endpoint struct {
	Address string `json:"address"`
}
type WebRTC struct {
	Pointer             *rtc.Target        `json:"pointer,omitempty"`
	PointerCapabilities []board.Capability `json:"pointerCapabilities,omitempty"`
	AudioCodec          string             `json:"audioCodec,omitempty"`
	TTLSeconds          int                `json:"ttlSeconds,omitempty"`
	ICEServers          []pion.ICEServer   `json:"iceServers,omitempty"`
	RelayOnly           bool               `json:"relayOnly,omitempty"`
	Video               *rtc.Target        `json:"video,omitempty"`
	Audio               *rtc.Target        `json:"audio,omitempty"`
	Microphone          *rtc.Target        `json:"microphone,omitempty"`
	Playback            *rtc.Target        `json:"playback,omitempty"`
	Input               *rtc.Target        `json:"input,omitempty"`
	InputCapabilities   []board.Capability `json:"inputCapabilities,omitempty"`
}

func Load(reader io.Reader) ([]board.Provider, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, errors.New("device configuration exceeds 1 MiB")
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	var tail any
	if err := dec.Decode(&tail); err != io.EOF {
		return nil, errors.New("invalid trailing device configuration")
	}
	return c.Providers()
}
func (c Config) Providers() ([]board.Provider, error) {
	if len(c.Devices) > 128 {
		return nil, errors.New("at most 128 device providers may be configured")
	}
	var result []board.Provider
	seen := map[string]bool{}
	for _, d := range c.Devices {
		if d.ID == "" || seen[d.ID] {
			return nil, errors.New("device provider IDs must be nonempty and unique")
		}
		seen[d.ID] = true
		n := 0
		for _, present := range []bool{d.Desktop != nil, d.Audio != nil, d.NativeAudio != nil, d.Pointer != nil, d.Storage != nil, d.Endpoint != nil, d.WebRTC != nil} {
			if present {
				n++
			}
		}
		if n != 1 {
			return nil, fmt.Errorf("provider %s needs exactly one device type", d.ID)
		}
		var p board.Provider
		var err error
		switch {
		case d.Desktop != nil:
			o := d.Desktop
			p, err = devices.Desktop(devices.DesktopOptions{ID: d.ID, Name: d.Name, Scope: o.Scope, Environment: o.Environment, Capture: o.Capture, Input: o.Input, Video: o.Video, Recording: o.Recording, Permissions: o.Permissions, RecordingDirectory: o.RecordingDirectory, MaxRecordingDuration: time.Duration(o.RecordingSeconds) * time.Second})
		case d.Pointer != nil:
			p, err = devices.NativePointer(d.ID, d.Name, d.Pointer.Helper, d.Pointer.Source)
		case d.NativeAudio != nil:
			o := d.NativeAudio
			p, err = devices.NativeAudio(devices.NativeAudioOptions{ID: d.ID, Name: d.Name, Helper: o.Helper, SystemSource: o.SystemSource, MicrophoneSource: o.MicrophoneSource, PlaybackSource: o.PlaybackSource, SystemCapture: o.SystemCapture, Microphone: o.Microphone, Playback: o.Playback})
		case d.Audio != nil:
			o := d.Audio
			p, err = devices.Audio(devices.AudioOptions{ID: d.ID, Name: d.Name, Socket: o.Socket, OutputDevice: o.OutputDevice, InputDevice: o.InputDevice, Playback: o.Playback, Microphone: o.Microphone, SystemCapture: o.SystemCapture, Configure: o.Configure})
		case d.Storage != nil:
			o := d.Storage
			p, err = devices.Storage(devices.StorageOptions{ID: d.ID, Name: d.Name, Path: o.Path, RootID: o.ResourceID, AllowRestore: o.AllowRestore, MaxTransferBytes: o.MaxTransferBytes, MaxExtractedBytes: o.MaxExtractedBytes})
		case d.Endpoint != nil:
			p, err = devices.Endpoint(devices.EndpointOptions{ID: d.ID, Name: d.Name, Address: d.Endpoint.Address})
		case d.WebRTC != nil:
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("provider %s: %w", d.ID, err)
		}
		result = append(result, p)
	}
	registry, err := board.NewRegistry(result...)
	if err != nil {
		return nil, err
	}
	listed, err := registry.List(context.Background())
	if err != nil {
		return nil, err
	}
	check := func(t *rtc.Target, cap board.Capability) error {
		if t == nil {
			return nil
		}
		for _, d := range listed {
			if d.ProviderID == t.ProviderID && d.ID == t.DeviceID {
				for _, a := range d.Capabilities {
					if a.Name == cap && !a.ResourceRequired {
						return nil
					}
				}
			}
		}
		return fmt.Errorf("WebRTC target %s/%s does not provide %s", t.ProviderID, t.DeviceID, cap)
	}
	for _, d := range c.Devices {
		w := d.WebRTC
		if w == nil {
			continue
		}
		if w.AudioCodec != "" && w.AudioCodec != "pcmu" && w.AudioCodec != "opus" {
			return nil, errors.New("audioCodec must be pcmu or opus")
		}
		audioSource, playback := rtc.BoardAudio, rtc.BoardPlayback
		if w.AudioCodec == "opus" {
			audioSource, playback = rtc.BoardOpusAudio, rtc.BoardOpusPlayback
		}
		o := rtc.Options{ID: d.ID, Name: d.Name, TTL: time.Duration(w.TTLSeconds) * time.Second, ICEServers: w.ICEServers, RelayOnly: w.RelayOnly}
		for _, v := range []struct {
			t   *rtc.Target
			cap board.Capability
		}{{w.Video, board.DisplayVideo}, {w.Audio, board.AudioCapture}, {w.Microphone, board.AudioMicrophone}, {w.Playback, board.AudioPlayback}} {
			if err := check(v.t, v.cap); err != nil {
				return nil, err
			}
		}
		if w.Video != nil {
			o.Video = rtc.BoardVideo(registry, *w.Video)
		}
		if w.Audio != nil {
			o.Audio = audioSource(registry, *w.Audio, board.AudioCapture)
		}
		if w.Microphone != nil {
			o.Microphone = audioSource(registry, *w.Microphone, board.AudioMicrophone)
		}
		if w.Playback != nil {
			o.Playback = playback(registry, *w.Playback)
		}
		if w.Input != nil {
			if len(w.InputCapabilities) == 0 {
				return nil, errors.New("WebRTC input requires explicit inputCapabilities")
			}
			for _, cap := range w.InputCapabilities {
				if err := check(w.Input, cap); err != nil {
					return nil, err
				}
			}
			o.InputCapabilities = w.InputCapabilities
			o.Input = rtc.BoardInput(registry, *w.Input)
		} else if len(w.InputCapabilities) > 0 {
			return nil, errors.New("inputCapabilities requires an input target")
		}
		if w.Pointer != nil {
			if len(w.PointerCapabilities) == 0 {
				return nil, errors.New("pointer target requires explicit pointerCapabilities")
			}
			for _, cap := range w.PointerCapabilities {
				if cap != board.PointerMove && cap != board.PointerClick && cap != board.PointerScroll && cap != board.PointerDown && cap != board.PointerUp && cap != board.PointerRelease {
					return nil, errors.New("invalid pointer capability")
				}
				if err := check(w.Pointer, cap); err != nil {
					return nil, err
				}
			}
			keyboard := o.Input
			factory := rtc.BoardInputSession(registry, *w.Pointer, w.PointerCapabilities)
			o.InputCapabilities = append(append([]board.Capability{}, o.InputCapabilities...), w.PointerCapabilities...)
			o.OpenInput = func(life context.Context, granted []board.Capability) (func(context.Context, board.Action) error, func() error, error) {
				pointer, closePointer, err := factory(life, granted)
				if err != nil {
					return nil, nil, err
				}
				return func(ctx context.Context, a board.Action) error {
					switch a.Capability {
					case board.PointerMove, board.PointerClick, board.PointerScroll, board.PointerDown, board.PointerUp, board.PointerRelease:
						return pointer(ctx, a)
					}
					if keyboard == nil {
						return board.ErrNotGranted
					}
					return keyboard(ctx, a)
				}, closePointer, nil
			}
		} else if len(w.PointerCapabilities) > 0 {
			return nil, errors.New("pointerCapabilities requires a pointer target")
		}
		p, err := rtc.New(o)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}
