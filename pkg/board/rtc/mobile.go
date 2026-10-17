package rtc

import (
	"board"
	"context"
	"encoding/binary"
	"errors"
	pion "github.com/pion/webrtc/v4"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// MobileMedia binds a single consented native producer to bounded Board media
// sources. The embedding host authenticates before Push and Close. Wire kinds:
// 1 JPEG video, 2 PCM system audio, 3 PCM microphone, 4 PCM playback to producer.
// PCM is always signed little-endian 16-bit stereo, 48 kHz.
type MobileMedia struct {
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	active   map[byte]chan []byte
	allowed  map[byte]bool
	playback chan []byte
}

func NewMobileMedia(video, audio, microphone, playback bool) *MobileMedia {
	ctx, cancel := context.WithCancel(context.Background())
	return &MobileMedia{ctx: ctx, cancel: cancel, active: map[byte]chan []byte{}, allowed: map[byte]bool{1: video, 2: audio, 3: microphone, 4: playback}, playback: make(chan []byte, 12)}
}
func (m *MobileMedia) Close()                  { m.cancel() }
func (m *MobileMedia) Done() <-chan struct{}   { return m.ctx.Done() }
func (m *MobileMedia) Playback() <-chan []byte { return m.playback }
func (m *MobileMedia) Push(kind byte, data []byte) error {
	if !m.allowed[kind] || kind == 4 {
		return board.ErrNotGranted
	}
	if kind == 1 {
		if len(data) == 0 || len(data) > 4<<20 {
			return board.ErrLimit
		}
	} else if len(data) == 0 || len(data) > 3840 || len(data)%4 != 0 {
		return board.ErrLimit
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return board.ErrClosed
	}
	queue := m.active[kind]
	if queue == nil {
		return nil
	} // No granted viewer: do not retain captured media.
	copyOfData := append([]byte(nil), data...)
	select {
	case queue <- copyOfData:
		return nil
	default:
	}
	if kind != 1 {
		return board.ErrLimit
	}
	select {
	case <-queue:
	default:
	}
	select {
	case queue <- copyOfData:
	default:
	}
	return nil
}
func (m *MobileMedia) Source(kind byte) *Source {
	if !m.allowed[kind] || kind < 1 || kind > 3 {
		return nil
	}
	codec := pion.RTPCodecCapability{MimeType: pion.MimeTypePCMU, ClockRate: 8000, Channels: 1}
	if kind == 1 {
		codec = pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8, ClockRate: 90000}
	}
	return &Source{Codec: codec, Open: func(ctx context.Context) (Samples, error) {
		m.mu.Lock()
		if m.ctx.Err() != nil {
			m.mu.Unlock()
			return nil, board.ErrClosed
		}
		if m.active[kind] != nil {
			m.mu.Unlock()
			return nil, errors.New("mobile media source unavailable or already attached")
		}
		size := 50
		if kind == 1 {
			size = 2
		}
		queue := make(chan []byte, size)
		m.active[kind] = queue
		m.mu.Unlock()
		detach := func() {
			m.mu.Lock()
			if m.active[kind] == queue {
				delete(m.active, kind)
			}
			m.mu.Unlock()
		}
		life, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(m.ctx, cancel)
		args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
		if kind == 1 {
			args = append(args, "-f", "image2pipe", "-framerate", "10", "-vcodec", "mjpeg", "-i", "pipe:0", "-an", "-c:v", "libvpx", "-deadline", "realtime", "-cpu-used", "8", "-f", "ivf", "pipe:1")
		} else {
			args = append(args, "-f", "s16le", "-ar", "48000", "-ac", "2", "-i", "pipe:0", "-f", "mulaw", "-ar", "8000", "-ac", "1", "pipe:1")
		}
		inputR, inputW, err := os.Pipe()
		if err != nil {
			stop()
			cancel()
			detach()
			return nil, err
		}
		outputR, outputW, err := os.Pipe()
		if err != nil {
			inputR.Close()
			inputW.Close()
			stop()
			cancel()
			detach()
			return nil, err
		}
		cmd := exec.CommandContext(life, "ffmpeg", args...)
		cmd.Stdin = inputR
		cmd.Stdout = outputW
		if err = cmd.Start(); err != nil {
			inputR.Close()
			inputW.Close()
			outputR.Close()
			outputW.Close()
			stop()
			cancel()
			detach()
			return nil, err
		}
		inputR.Close()
		outputW.Close()
		ch := &mobileStream{owner: m.ctx, reader: outputR, done: make(chan struct{}), cancel: cancel}
		cleanup := context.AfterFunc(life, func() { inputW.Close(); outputR.Close() })
		go func() {
			defer inputW.Close()
			var pace *time.Ticker
			if kind == 1 {
				pace = time.NewTicker(100 * time.Millisecond)
				defer pace.Stop()
			}
			for {
				select {
				case data := <-queue:
					if pace != nil {
						select {
						case <-pace.C:
						case <-life.Done():
							return
						}
					}
					if _, err := inputW.Write(data); err != nil {
						cancel()
						return
					}
				case <-life.Done():
					return
				}
			}
		}()
		go func() {
			_ = cmd.Wait()
			cancel()
			cleanup()
			inputW.Close()
			stop()
			detach()
			close(ch.done)
		}()
		if kind == 1 {
			var header [32]byte
			if _, err = io.ReadFull(ch, header[:]); err != nil {
				ch.Close()
				return nil, err
			}
			if string(header[:4]) != "DKIF" || string(header[8:12]) != "VP80" || binary.LittleEndian.Uint32(header[16:20]) != 10 {
				ch.Close()
				return nil, errors.New("invalid mobile VP8 stream")
			}
		}
		duration := 20 * time.Millisecond
		if kind == 1 {
			duration = 100 * time.Millisecond
		}
		return &samples{ch: ch, video: kind == 1, duration: duration}, nil
	}}
}

type mobileStream struct {
	owner  context.Context
	reader *os.File
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (s *mobileStream) Read(p []byte) (int, error) {
	n, err := s.reader.Read(p)
	if err != nil && s.owner.Err() != nil {
		return n, board.ErrClosed
	}
	return n, err
}
func (s *mobileStream) Write([]byte) (int, error) { return 0, board.ErrNotGranted }
func (s *mobileStream) Close() error {
	s.once.Do(func() { s.cancel(); s.reader.Close(); <-s.done })
	return nil
}

// PlaybackSource translates PCMU to fixed PCM packets through the same bounded
// native channel shape used by the phone sink.
func (m *MobileMedia) PlaybackSource() *Playback {
	if !m.allowed[4] {
		return nil
	}
	return &Playback{Codec: pion.RTPCodecCapability{MimeType: pion.MimeTypePCMU, ClockRate: 8000, Channels: 1}, Open: func(ctx context.Context) (io.WriteCloser, error) { return &mobilePlayback{owner: m, ctx: ctx}, nil }}
}

type mobilePlayback struct {
	owner *MobileMedia
	ctx   context.Context
}

func (p *mobilePlayback) Close() error { return nil }
func (p *mobilePlayback) Write(data []byte) (int, error) {
	if len(data) > 160 {
		return 0, board.ErrLimit
	}
	// G.711 expansion; six 48 kHz samples per 8 kHz sample, in both channels.
	out := make([]byte, len(data)*24)
	for i, value := range data {
		v := ^value
		t := ((int(v&15) << 3) + 132) << ((v >> 4) & 7)
		sample := t - 132
		if v&128 != 0 {
			sample = 132 - t
		}
		for j := 0; j < 12; j++ {
			binary.LittleEndian.PutUint16(out[i*24+j*2:], uint16(int16(sample)))
		}
	}
	select {
	case p.owner.playback <- out:
		return len(data), nil
	case <-p.ctx.Done():
		return 0, p.ctx.Err()
	case <-p.owner.ctx.Done():
		return 0, board.ErrClosed
	}
}
