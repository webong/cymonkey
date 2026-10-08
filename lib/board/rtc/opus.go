package rtc

import (
	"board"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/pion/rtp"
	pion "github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

func OpusCodec() pion.RTPCodecCapability {
	return pion.RTPCodecCapability{MimeType: pion.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"}
}

// BoardOpusAudio encodes the granted PCM channel with a local libopus-enabled
// ffmpeg. Source IDs and process arguments are never supplied by remote peers.
func BoardOpusAudio(r *board.Registry, t Target, cap board.Capability) *Source {
	return &Source{Codec: OpusCodec(), Open: func(ctx context.Context) (Samples, error) {
		if cap != board.AudioCapture && cap != board.AudioMicrophone {
			return nil, board.ErrNotGranted
		}
		ch, err := channelEncoding(ctx, r, t, cap, false, "pcm")
		if err != nil {
			return nil, err
		}
		process, err := opusProcess(ctx, ch, true)
		if err != nil {
			return nil, err
		}
		return &opusSamples{process: process}, nil
	}}
}
func BoardOpusPlayback(r *board.Registry, t Target) *Playback {
	return &Playback{Codec: OpusCodec(), Open: func(ctx context.Context) (io.WriteCloser, error) {
		ch, err := channelEncoding(ctx, r, t, board.AudioPlayback, true, "pcm")
		if err != nil {
			return nil, err
		}
		process, err := opusProcess(ctx, ch, false)
		if err != nil {
			return nil, err
		}
		ogg, err := oggwriter.NewWith(process.pipe, 48000, 2)
		if err != nil {
			process.Close()
			return nil, err
		}
		return &opusPlayback{process: process, ogg: ogg}, nil
	}}
}

type opusCommand struct {
	pipe    *os.File
	channel *ownedChannel
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
}

func (c *opusCommand) Close() error {
	c.once.Do(func() { c.cancel(); c.pipe.Close(); c.channel.Close(); <-c.done })
	return nil
}
func opusProcess(ctx context.Context, ch *ownedChannel, encode bool) (*opusCommand, error) {
	life, cancel := context.WithCancel(ctx)
	reader, writer, err := os.Pipe()
	if err != nil {
		cancel()
		ch.Close()
		return nil, err
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if encode {
		args = append(args, "-f", "s16le", "-ar", "48000", "-ac", "2", "-i", "pipe:0", "-c:a", "libopus", "-application", "voip", "-frame_duration", "20", "-b:a", "64000", "-fec", "1", "-packet_loss", "10", "-f", "opus", "-page_duration", "20000", "-flush_packets", "1", "pipe:1")
	} else {
		args = append(args, "-probesize", "32768", "-analyzeduration", "0", "-f", "ogg", "-i", "pipe:0", "-f", "s16le", "-ar", "48000", "-ac", "2", "-flush_packets", "1", "pipe:1")
	}
	cmd := exec.CommandContext(life, "ffmpeg", args...)
	cmd.WaitDelay = 2 * time.Second
	c := &opusCommand{channel: ch, cancel: cancel, done: make(chan struct{})}
	if encode {
		cmd.Stdin = ch
		cmd.Stdout = writer
		c.pipe = reader
	} else {
		cmd.Stdin = reader
		cmd.Stdout = ch
		c.pipe = writer
	}
	if err = cmd.Start(); err != nil {
		cancel()
		reader.Close()
		writer.Close()
		ch.Close()
		return nil, errors.New("Opus converter unavailable")
	}
	if encode {
		writer.Close()
	} else {
		reader.Close()
	}
	stop := context.AfterFunc(life, func() { ch.Close(); c.pipe.Close() })
	go func() { _ = cmd.Wait(); cancel(); stop(); ch.Close(); close(c.done) }()
	return c, nil
}

type opusPlayback struct {
	process *opusCommand
	ogg     *oggwriter.OggWriter
}

// Each Write is exactly one Opus packet. Cancellation closes the process pipe
// before any Ogg finalization, so a blocked writer cannot hold session teardown.
func (p *opusPlayback) Write(b []byte) (int, error) {
	if _, err := opusDuration(b); err != nil {
		return 0, err
	}
	if err := p.ogg.WriteRTP(&rtp.Packet{Payload: b}); err != nil {
		return 0, err
	}
	return len(b), nil
}
func (p *opusPlayback) Close() error { return p.process.Close() }

type opusSamples struct {
	process *opusCommand
	partial []byte
	packets [][]byte
	headers int
}

func (s *opusSamples) Close() error { return s.process.Close() }
func (s *opusSamples) ReadSample(ctx context.Context) (media.Sample, error) {
	for len(s.packets) == 0 {
		if err := ctx.Err(); err != nil {
			return media.Sample{}, err
		}
		var header [27]byte
		if _, err := io.ReadFull(s.process.pipe, header[:]); err != nil {
			return media.Sample{}, err
		}
		if string(header[:4]) != "OggS" || header[4] != 0 || header[5]&1 != 0 && len(s.partial) == 0 {
			return media.Sample{}, errors.New("invalid Opus page")
		}
		laces := make([]byte, int(header[26]))
		if _, err := io.ReadFull(s.process.pipe, laces); err != nil {
			return media.Sample{}, err
		}
		for _, size := range laces {
			if len(s.partial)+int(size) > 64<<10 {
				return media.Sample{}, board.ErrLimit
			}
			segment := make([]byte, int(size))
			if _, err := io.ReadFull(s.process.pipe, segment); err != nil {
				return media.Sample{}, err
			}
			s.partial = append(s.partial, segment...)
			if size == 255 {
				continue
			}
			packet := s.partial
			s.partial = nil
			if s.headers == 0 {
				if len(packet) < 19 || string(packet[:8]) != "OpusHead" || packet[9] != 2 {
					return media.Sample{}, errors.New("invalid Opus identification")
				}
				s.headers++
				continue
			}
			if s.headers == 1 {
				if len(packet) < 8 || string(packet[:8]) != "OpusTags" {
					return media.Sample{}, errors.New("invalid Opus tags")
				}
				s.headers++
				continue
			}
			if len(packet) > 4096 {
				return media.Sample{}, board.ErrLimit
			}
			s.packets = append(s.packets, packet)
		}
	}
	packet := s.packets[0]
	s.packets = s.packets[1:]
	duration, err := opusDuration(packet)
	return media.Sample{Data: packet, Duration: duration}, err
}

// RFC 6716 TOC frame duration/count; all Opus RTP timestamps use 48 kHz.
func opusDuration(packet []byte) (time.Duration, error) {
	if len(packet) < 1 || len(packet) > 4096 {
		return 0, errors.New("invalid Opus packet")
	}
	config := packet[0] >> 3
	var micros int
	if config >= 16 {
		micros = 2500 << uint(config&3)
	} else if config >= 12 {
		micros = 10000 << uint(config&1)
	} else {
		micros = []int{10000, 20000, 40000, 60000}[config&3]
	}
	count := 1
	switch packet[0] & 3 {
	case 1, 2:
		count = 2
	case 3:
		if len(packet) < 2 {
			return 0, io.ErrUnexpectedEOF
		}
		count = int(packet[1] & 63)
	}
	if count == 0 || micros*count > 120000 {
		return 0, errors.New("invalid Opus duration")
	}
	return time.Duration(micros*count) * time.Microsecond, nil
}
