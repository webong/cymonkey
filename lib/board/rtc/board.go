package rtc

import (
	"board"
	"context"
	"encoding/binary"
	"errors"
	pion "github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"io"
	"sync"
	"time"
)

// Target is a host-selected device. A remote signal cannot change it.
type Target struct {
	ProviderID string `json:"providerId"`
	DeviceID   string `json:"deviceId"`
}

func openTarget(ctx context.Context, r *board.Registry, t Target, cap board.Capability) (*board.Connection, error) {
	return r.Open(ctx, board.OpenRequest{ProviderID: t.ProviderID, DeviceID: t.DeviceID, Grant: board.Grant{Capabilities: []board.Capability{cap}}})
}

type ownedChannel struct {
	io.ReadWriteCloser
	connection *board.Connection
}

func (c *ownedChannel) Close() error {
	return errors.Join(c.ReadWriteCloser.Close(), c.connection.Close(context.Background()))
}
func channel(ctx context.Context, r *board.Registry, t Target, cap board.Capability, write bool) (*ownedChannel, error) {
	return channelEncoding(ctx, r, t, cap, write, "pcmu")
}
func channelEncoding(ctx context.Context, r *board.Registry, t Target, cap board.Capability, write bool, encoding string) (*ownedChannel, error) {
	c, err := openTarget(ctx, r, t, cap)
	if err != nil {
		return nil, err
	}
	a := board.Action{Capability: cap}
	if cap != board.DisplayVideo {
		a.Input = []byte(`{"encoding":"` + encoding + `"}`)
	}
	l := board.ChannelLimits{ReadBytes: 1 << 30, Duration: time.Hour}
	if write {
		l.ReadBytes = 0
		l.WriteBytes = 1 << 30
	}
	ch, err := c.OpenChannel(ctx, a, l)
	if err != nil {
		_ = c.Close(context.Background())
		return nil, err
	}
	return &ownedChannel{ch, c}, nil
}

// BoardVideo consumes the bounded VP8/IVF channel exposed by devices.Desktop.
func BoardVideo(r *board.Registry, t Target) *Source {
	return &Source{Codec: pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8, ClockRate: 90000}, Open: func(ctx context.Context) (Samples, error) {
		ch, err := channel(ctx, r, t, board.DisplayVideo, false)
		if err != nil {
			return nil, err
		}
		var h [32]byte
		if _, err = io.ReadFull(ch, h[:]); err != nil {
			ch.Close()
			return nil, err
		}
		rate, scale := binary.LittleEndian.Uint32(h[16:20]), binary.LittleEndian.Uint32(h[20:24])
		if string(h[:4]) != "DKIF" || string(h[8:12]) != "VP80" || binary.LittleEndian.Uint16(h[6:8]) != 32 || rate == 0 || scale == 0 {
			ch.Close()
			return nil, errors.New("invalid VP8 IVF header")
		}
		duration := time.Duration(float64(time.Second) * float64(scale) / float64(rate))
		if duration <= 0 || duration > time.Second {
			ch.Close()
			return nil, errors.New("invalid IVF frame rate")
		}
		return &samples{ch: ch, video: true, duration: duration}, nil
	}}
}
func BoardAudio(r *board.Registry, t Target, cap board.Capability) *Source {
	return &Source{Codec: pion.RTPCodecCapability{MimeType: pion.MimeTypePCMU, ClockRate: 8000, Channels: 1}, Open: func(ctx context.Context) (Samples, error) {
		if cap != board.AudioCapture && cap != board.AudioMicrophone {
			return nil, board.ErrNotGranted
		}
		ch, err := channel(ctx, r, t, cap, false)
		if err != nil {
			return nil, err
		}
		return &samples{ch: ch, duration: 20 * time.Millisecond}, nil
	}}
}
func BoardPlayback(r *board.Registry, t Target) *Playback {
	return &Playback{Codec: pion.RTPCodecCapability{MimeType: pion.MimeTypePCMU, ClockRate: 8000, Channels: 1}, Open: func(ctx context.Context) (io.WriteCloser, error) {
		return channel(ctx, r, t, board.AudioPlayback, true)
	}}
}
func BoardInput(r *board.Registry, t Target) func(context.Context, board.Action) error {
	return func(ctx context.Context, a board.Action) error {
		if !inputCapability(a.Capability) {
			return board.ErrNotGranted
		}
		c, err := openTarget(ctx, r, t, a.Capability)
		if err != nil {
			return err
		}
		defer c.Close(context.Background())
		_, err = c.Invoke(ctx, a)
		return err
	}
}

type samples struct {
	ch       io.ReadWriteCloser
	video    bool
	duration time.Duration
}

func (s *samples) Close() error { return s.ch.Close() }
func (s *samples) ReadSample(ctx context.Context) (media.Sample, error) {
	if err := ctx.Err(); err != nil {
		return media.Sample{}, err
	}
	size := uint32(160)
	if s.video {
		var h [12]byte
		if _, err := io.ReadFull(s.ch, h[:]); err != nil {
			return media.Sample{}, err
		}
		size = binary.LittleEndian.Uint32(h[:4])
		if size == 0 || size > 8<<20 {
			return media.Sample{}, board.ErrLimit
		}
	}
	data := make([]byte, int(size))
	_, err := io.ReadFull(s.ch, data)
	return media.Sample{Data: data, Duration: s.duration}, err
}

func inputCapability(cap board.Capability) bool {
	switch cap {
	case board.KeyboardPress, board.KeyboardType, board.PointerMove, board.PointerClick, board.PointerScroll, board.PointerDown, board.PointerUp, board.PointerRelease:
		return true
	}
	return false
}

// BoardInputSession retains one input connection for the peer lifetime. Opening
// is lazy so view-only peers do not occupy the pointer. No grants are widened.
func BoardInputSession(r *board.Registry, t Target, caps []board.Capability) func(context.Context, []board.Capability) (func(context.Context, board.Action) error, func() error, error) {
	return func(life context.Context, granted []board.Capability) (func(context.Context, board.Action) error, func() error, error) {
		var mu sync.Mutex
		var connection *board.Connection
		closed := false
		allowed := []board.Capability{}
		for _, cap := range caps {
			for _, grant := range granted {
				if cap == grant {
					allowed = append(allowed, cap)
					break
				}
			}
		}
		has := map[board.Capability]bool{}
		for _, cap := range allowed {
			has[cap] = true
		}
		if has[board.PointerDown] && (!has[board.PointerMove] || !has[board.PointerUp] || !has[board.PointerRelease]) {
			return nil, nil, errors.New("held input requires pointer.move, pointer.up and pointer.release grants")
		}
		closeInput := func() error {
			mu.Lock()
			defer mu.Unlock()
			closed = true
			if connection != nil {
				return connection.Close(context.Background())
			}
			return nil
		}
		invoke := func(ctx context.Context, a board.Action) error {
			mu.Lock()
			defer mu.Unlock()
			if closed || life.Err() != nil {
				return board.ErrClosed
			}
			found := false
			for _, cap := range allowed {
				if a.Capability == cap {
					found = true
				}
			}
			if !found {
				return board.ErrNotGranted
			}
			if connection == nil {
				var err error
				connection, err = r.Open(ctx, board.OpenRequest{ProviderID: t.ProviderID, DeviceID: t.DeviceID, Grant: board.Grant{Capabilities: allowed}})
				if err != nil {
					return err
				}
			}
			_, err := connection.Invoke(ctx, a)
			return err
		}
		return invoke, closeInput, nil
	}
}
