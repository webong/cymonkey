package rtc

import (
	"context"
	"github.com/pion/rtp"
	pion "github.com/pion/webrtc/v4"
	"io"
	"time"
)

// playPCMU uses wrapping RTP timestamps rather than arrival order. Startup is
// delayed 60 ms; a 200 ms window bounds memory. Missing samples become silence.
func playPCMU(ctx context.Context, track *pion.TrackRemote, writer io.Writer) error {
	packets := make(chan *rtp.Packet, 32)
	failures := make(chan error, 1)
	go func() {
		for {
			packet, _, err := track.ReadRTP()
			if err != nil {
				failures <- err
				return
			}
			if len(packet.Payload) == 0 || len(packet.Payload) > 1600 {
				continue
			}
			select {
			case packets <- packet:
			case <-ctx.Done():
				return
			}
		}
	}()
	buffer := make(map[uint32]byte, 1600)
	var next uint32
	initialized, playing := false, false
	var start <-chan time.Time
	var startup *time.Timer
	defer func() {
		if startup != nil {
			startup.Stop()
		}
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	emptyFrames := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-failures:
			return err
		case packet := <-packets:
			if !initialized {
				next = packet.Timestamp
				initialized = true
				if !playing {
					startup = time.NewTimer(60 * time.Millisecond)
					start = startup.C
				}
			}
			for i, b := range packet.Payload {
				timestamp := packet.Timestamp + uint32(i)
				delta := int32(timestamp - next)
				if delta >= 0 && delta < 1600 {
					if _, exists := buffer[timestamp]; !exists {
						buffer[timestamp] = b
					}
				}
			}
		case <-start:
			playing = true
			start = nil
			ticker.Reset(20 * time.Millisecond)
		case <-ticker.C:
			if !playing {
				continue
			}
			var frame [160]byte
			found := 0
			for i := range frame {
				frame[i] = 0xff
				if b, ok := buffer[next]; ok {
					frame[i] = b
					delete(buffer, next)
					found++
				}
				next++
			}
			if found == 0 {
				emptyFrames++
			} else {
				emptyFrames = 0
			}
			if emptyFrames >= 50 {
				clear(buffer)
				initialized = false
				emptyFrames = 0
			}
			n, err := writer.Write(frame[:])
			if err != nil {
				return err
			}
			if n != len(frame) {
				return io.ErrShortWrite
			}
		}
	}
}

// Opus packets are kept intact for the decoder. This receive path inserts a
// 20 ms Opus silence packet for missing timestamps; it does not claim FEC decode.
func playOpus(ctx context.Context, track *pion.TrackRemote, writer io.Writer) error {
	packets := make(chan *rtp.Packet, 32)
	failures := make(chan error, 1)
	go func() {
		for {
			packet, _, err := track.ReadRTP()
			if err != nil {
				failures <- err
				return
			}
			if _, err := opusDuration(packet.Payload); err != nil {
				continue
			}
			select {
			case packets <- packet:
			case <-ctx.Done():
				return
			}
		}
	}()
	pending := map[uint32]*rtp.Packet{}
	var next uint32
	initialized := false
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	var tick <-chan time.Time
	empty := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-failures:
			return err
		case packet := <-packets:
			if !initialized {
				next = packet.Timestamp
				initialized = true
				timer.Reset(60 * time.Millisecond)
				tick = timer.C
			}
			delta := int32(packet.Timestamp - next)
			if delta >= 0 && delta < 9600 && len(pending) < 32 {
				if _, exists := pending[packet.Timestamp]; !exists {
					pending[packet.Timestamp] = packet
				}
			}
		case <-tick:
			payload := []byte{0xf8, 0xff, 0xfe}
			duration := 20 * time.Millisecond
			if packet, ok := pending[next]; ok {
				payload = packet.Payload
				duration, _ = opusDuration(payload)
				empty = 0
			} else {
				empty++
			}
			delete(pending, next)
			next += uint32(duration * 48000 / time.Second)
			for timestamp := range pending {
				if int32(timestamp-next) < 0 {
					delete(pending, timestamp)
				}
			}
			n, err := writer.Write(payload)
			if err != nil {
				return err
			}
			if n != len(payload) {
				return io.ErrShortWrite
			}
			if empty >= 50 {
				clear(pending)
				initialized = false
				empty = 0
				tick = nil
			} else {
				timer.Reset(duration)
			}
		}
	}
}
