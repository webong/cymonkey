// Package rtc connects authorized Board device sessions through Pion WebRTC.
package rtc

import (
	"board"
	"board/device"
	"context"
	"encoding/json"
	"errors"
	pion "github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"io"
	"strings"
	"sync"
	"time"
)

// Samples supplies encoded media. Close must unblock ReadSample.
type Samples interface {
	ReadSample(context.Context) (media.Sample, error)
	Close() error
}
type Source struct {
	Codec pion.RTPCodecCapability
	Open  func(context.Context) (Samples, error)
}
type Playback struct {
	Codec pion.RTPCodecCapability
	Open  func(context.Context) (io.WriteCloser, error)
}
type Options struct {
	ID, Name                 string
	TTL                      time.Duration
	ICEServers               []pion.ICEServer
	RelayOnly                bool
	API                      *pion.API
	Video, Audio, Microphone *Source
	Playback                 *Playback
	InputCapabilities        []board.Capability
	Input                    func(context.Context, board.Action) error
}

// Signal is scoped to the already-authorized Board session. SDP and candidates
// are private connection material and must not enter lifecycle logs.
type Signal struct {
	Type      string                 `json:"type"`
	SDP       string                 `json:"sdp,omitempty"`
	Candidate *pion.ICECandidateInit `json:"candidate,omitempty"`
	At        time.Time              `json:"at,omitempty"`
}

const signalSchema = `{"type":"object","properties":{"type":{"enum":["offer","answer","ice-candidate","restart","bye","heartbeat"]},"sdp":{"type":"string"},"candidate":{"type":"object"}},"required":["type"],"additionalProperties":false}`

func New(o Options) (*device.Provider, error) {
	if o.ID == "" {
		return nil, errors.New("WebRTC provider needs an ID")
	}
	if o.TTL == 0 {
		o.TTL = 5 * time.Minute
	}
	if o.TTL <= 0 || o.TTL > time.Hour {
		return nil, errors.New("WebRTC lifetime must be at most one hour")
	}
	if o.API == nil {
		var settings pion.SettingEngine
		settings.SetSCTPMaxMessageSize(64 << 10)
		settings.SetSCTPMaxReceiveBufferSize(1 << 20)
		o.API = pion.NewAPI(pion.WithSettingEngine(settings))
	}
	caps := []board.CapabilityDescriptor{device.Descriptor(board.WebRTCSignal, signalSchema, false), device.Descriptor(board.WebRTCPoll, device.EmptySchema, false), device.Descriptor(board.DeviceHealth, device.EmptySchema, false)}
	for cap, source := range map[board.Capability]*Source{board.DisplayVideo: o.Video, board.AudioCapture: o.Audio, board.AudioMicrophone: o.Microphone} {
		if source != nil {
			if source.Open == nil || source.Codec.MimeType == "" {
				return nil, errors.New("WebRTC source needs a codec and opener")
			}
			caps = append(caps, device.Descriptor(cap, device.EmptySchema, false))
		}
	}
	if o.Playback != nil {
		if o.Playback.Open == nil || (!strings.EqualFold(o.Playback.Codec.MimeType, pion.MimeTypePCMU) && !strings.EqualFold(o.Playback.Codec.MimeType, pion.MimeTypeOpus)) {
			return nil, errors.New("WebRTC playback requires PCMU or Opus audio")
		}
		caps = append(caps, device.Descriptor(board.AudioPlayback, device.EmptySchema, false))
	}
	for _, cap := range o.InputCapabilities {
		if o.Input == nil || !inputCapability(cap) {
			return nil, errors.New("WebRTC input requires a bound input handler")
		}
		caps = append(caps, device.Descriptor(cap, `{"type":"object"}`, false))
	}
	return &device.Provider{ProviderID: o.ID, Device: board.Device{ID: "peer", Kind: "webrtc", Name: o.Name, Capabilities: caps}, Bind: func(life context.Context, g board.Grant) (device.Binding, error) {
		allowed := map[board.Capability]bool{}
		for _, cap := range g.Capabilities {
			allowed[cap] = true
		}
		if !allowed[board.WebRTCSignal] || !allowed[board.WebRTCPoll] {
			return device.Binding{}, errors.New("WebRTC requires signaling and polling grants")
		}
		p, err := openPeer(life, o, allowed)
		if err != nil {
			return device.Binding{}, err
		}
		ops := map[board.Capability]device.Operation{
			board.WebRTCSignal: {Invoke: func(ctx context.Context, a board.Action) (board.Result, error) {
				if len(a.Input) > 512<<10 {
					return board.Result{}, board.ErrLimit
				}
				var in Signal
				if err := decodeSignal(a.Input, &in); err != nil {
					return board.Result{}, err
				}
				if err := p.signal(ctx, in); err != nil {
					return board.Result{}, err
				}
				return device.Result(map[string]bool{"accepted": true})
			}},
			board.WebRTCPoll: {Invoke: func(_ context.Context, a board.Action) (board.Result, error) {
				if err := device.Empty(a.Input); err != nil {
					return board.Result{}, err
				}
				p.mu.Lock()
				defer p.mu.Unlock()
				result := append([]Signal{}, p.queue...)
				p.queue = nil
				return device.Result(result)
			}},
			board.DeviceHealth: {Invoke: func(_ context.Context, a board.Action) (board.Result, error) {
				if err := device.Empty(a.Input); err != nil {
					return board.Result{}, err
				}
				return device.Result(map[string]string{"state": p.pc.ConnectionState().String()})
			}},
		}
		for _, cap := range o.InputCapabilities {
			ops[cap] = device.Operation{Invoke: func(ctx context.Context, a board.Action) (board.Result, error) {
				if err := p.input(ctx, a); err != nil {
					return board.Result{}, err
				}
				return device.Result(map[string]bool{"submitted": true})
			}}
		}
		// Media capabilities authorize track setup, never raw action callbacks.
		for _, cap := range []board.Capability{board.DisplayVideo, board.AudioCapture, board.AudioMicrophone, board.AudioPlayback} {
			ops[cap] = device.Operation{Invoke: func(context.Context, board.Action) (board.Result, error) {
				return device.Result(map[string]string{"transport": "webrtc"})
			}}
		}
		return device.Binding{Operations: ops, Events: p.events, Close: func(context.Context) error { return p.close() }}, nil
	}}, nil
}

type sending struct {
	source *Source
	track  *pion.TrackLocalStaticSample
}
type peer struct {
	pc           *pion.PeerConnection
	o            Options
	allowed      map[board.Capability]bool
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	signaling    sync.Mutex
	queue        []Signal
	pending      []pion.ICECandidateInit
	remote       bool
	closed       bool
	inputs       chan board.Action
	events       chan board.Event
	sources      []sending
	start        sync.Once
	closeOnce    sync.Once
	playbackMu   sync.Mutex
	playing      bool
	inputChannel bool
}

func openPeer(parent context.Context, o Options, allowed map[board.Capability]bool) (*peer, error) {
	ctx, cancel := context.WithTimeout(parent, o.TTL)
	config := pion.Configuration{ICEServers: o.ICEServers}
	if o.RelayOnly {
		config.ICETransportPolicy = pion.ICETransportPolicyRelay
	}
	pc, err := o.API.NewPeerConnection(config)
	if err != nil {
		cancel()
		return nil, err
	}
	p := &peer{pc: pc, o: o, allowed: allowed, ctx: ctx, cancel: cancel, inputs: make(chan board.Action, 32), events: make(chan board.Event, 64)}
	pc.OnICECandidate(func(c *pion.ICECandidate) {
		if c != nil {
			v := c.ToJSON()
			p.enqueue(Signal{Type: "ice-candidate", Candidate: &v})
		}
	})
	pc.OnConnectionStateChange(func(state pion.PeerConnectionState) {
		p.emit("transport." + state.String())
		if state == pion.PeerConnectionStateConnected {
			p.start.Do(func() {
				for _, src := range p.sources {
					go p.send(src)
				}
			})
		}
		if state == pion.PeerConnectionStateFailed {
			go p.close()
		}
	})
	fail := func(err error) (*peer, error) { _ = p.close(); return nil, err }
	for _, entry := range []struct {
		cap    board.Capability
		source *Source
	}{{board.DisplayVideo, o.Video}, {board.AudioCapture, o.Audio}, {board.AudioMicrophone, o.Microphone}} {
		if !allowed[entry.cap] || entry.source == nil {
			continue
		}
		track, err := pion.NewTrackLocalStaticSample(entry.source.Codec, string(entry.cap), "board")
		if err != nil {
			return fail(err)
		}
		transceiver, err := pc.AddTransceiverFromTrack(track, pion.RTPTransceiverInit{Direction: pion.RTPTransceiverDirectionSendonly})
		if err != nil {
			return fail(err)
		}
		go func() {
			buf := make([]byte, 1500)
			for {
				if _, _, err := transceiver.Sender().Read(buf); err != nil {
					return
				}
			}
		}()
		p.sources = append(p.sources, sending{entry.source, track})
	}
	if allowed[board.AudioPlayback] && o.Playback != nil {
		tr, err := pc.AddTransceiverFromKind(pion.RTPCodecTypeAudio, pion.RTPTransceiverInit{Direction: pion.RTPTransceiverDirectionRecvonly})
		if err != nil {
			return fail(err)
		}
		payloadType := pion.PayloadType(0)
		if strings.EqualFold(o.Playback.Codec.MimeType, pion.MimeTypeOpus) {
			payloadType = 111
		}
		if err := tr.SetCodecPreferences([]pion.RTPCodecParameters{{RTPCodecCapability: o.Playback.Codec, PayloadType: payloadType}}); err != nil {
			return fail(err)
		}
	}
	pc.OnTrack(func(track *pion.TrackRemote, _ *pion.RTPReceiver) { go p.receive(track) })
	pc.OnDataChannel(func(d *pion.DataChannel) {
		if d.Label() != "board.input" || o.Input == nil {
			_ = d.Close()
			return
		}
		p.mu.Lock()
		if p.inputChannel {
			p.mu.Unlock()
			_ = d.Close()
			return
		}
		p.inputChannel = true
		p.mu.Unlock()
		d.OnClose(func() { p.mu.Lock(); p.inputChannel = false; p.mu.Unlock() })
		d.OnMessage(func(m pion.DataChannelMessage) {
			if len(m.Data) > 64<<10 {
				p.emit("input.denied")
				_ = d.Close()
				return
			}
			var a board.Action
			if err := device.Decode(m.Data, &a); err != nil {
				p.emit("input.denied")
				return
			}
			if !p.allowed[a.Capability] || a.ResourceID != "" || !inputCapability(a.Capability) {
				p.emit("input.denied")
				return
			}
			select {
			case p.inputs <- a:
			case <-ctx.Done():
			default:
				p.emit("input.overflow")
				go p.close()
			}
		})
	})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case a := <-p.inputs:
				if err := p.input(ctx, a); err != nil {
					p.emit("input.failed")
				} else {
					p.emit("input.submitted")
				}
			}
		}
	}()
	go func() { <-ctx.Done(); _ = p.close() }()
	return p, nil
}
func (p *peer) emit(kind string) {
	select {
	case p.events <- board.Event{Type: kind}:
	default:
	}
}
func (p *peer) enqueue(s Signal) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	if len(p.queue) >= 128 {
		p.mu.Unlock()
		p.emit("signaling.overflow")
		go p.close()
		return
	}
	s.At = time.Now().UTC()
	p.queue = append(p.queue, s)
	p.mu.Unlock()
}
func (p *peer) input(ctx context.Context, a board.Action) error {
	if p.ctx.Err() != nil {
		return board.ErrClosed
	}
	if !p.allowed[a.Capability] || p.o.Input == nil || a.ResourceID != "" || !inputCapability(a.Capability) {
		return board.ErrNotGranted
	}
	call, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	return p.o.Input(call, a)
}
func (p *peer) send(src sending) {
	reader, err := src.source.Open(p.ctx)
	if err != nil {
		if errors.Is(err, board.ErrClosed) {
			go p.close()
		}
		p.emit("media.failed")
		return
	}
	defer reader.Close()
	stop := context.AfterFunc(p.ctx, func() { _ = reader.Close() })
	defer stop()
	for {
		sample, err := reader.ReadSample(p.ctx)
		if err != nil {
			if errors.Is(err, board.ErrClosed) {
				go p.close()
			}
			if p.ctx.Err() == nil {
				p.emit("media.ended")
			}
			return
		}
		if len(sample.Data) > 8<<20 || sample.Duration <= 0 {
			p.emit("media.failed")
			return
		}
		if err = src.track.WriteSample(sample); err != nil {
			p.emit("media.failed")
			return
		}
	}
}
func (p *peer) receive(track *pion.TrackRemote) {
	if !p.allowed[board.AudioPlayback] || p.o.Playback == nil || !strings.EqualFold(track.Codec().MimeType, p.o.Playback.Codec.MimeType) {
		p.emit("media.denied")
		_ = p.close()
		return
	}
	p.playbackMu.Lock()
	if p.playing {
		p.playbackMu.Unlock()
		p.emit("media.denied")
		_ = p.close()
		return
	}
	p.playing = true
	p.playbackMu.Unlock()
	defer func() { p.playbackMu.Lock(); p.playing = false; p.playbackMu.Unlock() }()
	writer, err := p.o.Playback.Open(p.ctx)
	if err != nil {
		p.emit("media.failed")
		return
	}
	defer writer.Close()
	stop := context.AfterFunc(p.ctx, func() { _ = writer.Close() })
	defer stop()
	// Closing also unblocks the RTP reader if native playback fails.
	defer p.close()
	play := playPCMU
	if strings.EqualFold(track.Codec().MimeType, pion.MimeTypeOpus) {
		play = playOpus
	}
	if err := play(p.ctx, track, writer); err != nil && p.ctx.Err() == nil {
		p.emit("media.failed")
	}
}
func (p *peer) close() error {
	var err error
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.queue = nil
		p.pending = nil
		p.mu.Unlock()
		p.cancel()
		err = p.pc.Close()
		p.emit("transport.closed")
	})
	return err
}
func decodeSignal(raw json.RawMessage, in *Signal) error {
	// SDP may exceed ordinary action metadata; validate the envelope separately.
	if len(raw) > 512<<10 {
		return board.ErrLimit
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(in); err != nil {
		return err
	}
	var tail any
	if err := dec.Decode(&tail); !errors.Is(err, io.EOF) {
		return errors.New("invalid signaling envelope")
	}
	return nil
}
func (p *peer) signal(ctx context.Context, s Signal) error {
	p.signaling.Lock()
	defer p.signaling.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.ctx.Err() != nil {
		return board.ErrClosed
	}
	switch s.Type {
	case "heartbeat":
		return nil
	case "bye":
		return p.close()
	case "ice-candidate":
		if s.Candidate == nil || len(s.Candidate.Candidate) > 8192 {
			return errors.New("invalid ICE candidate")
		}
		if !p.remote {
			p.mu.Lock()
			defer p.mu.Unlock()
			if len(p.pending) >= 128 {
				return board.ErrLimit
			}
			p.pending = append(p.pending, *s.Candidate)
			return nil
		}
		return p.pc.AddICECandidate(*s.Candidate)
	case "offer", "restart":
		if s.SDP == "" {
			offer, err := p.pc.CreateOffer(&pion.OfferOptions{ICERestart: s.Type == "restart"})
			if err != nil {
				return err
			}
			if err = p.pc.SetLocalDescription(offer); err != nil {
				return err
			}
			p.enqueue(Signal{Type: "offer", SDP: offer.SDP})
			return nil
		}
		if s.Type == "restart" {
			return errors.New("restart creates a local offer")
		}
		if err := p.remoteDescription(pion.SessionDescription{Type: pion.SDPTypeOffer, SDP: s.SDP}); err != nil {
			return err
		}
		answer, err := p.pc.CreateAnswer(nil)
		if err != nil {
			return err
		}
		if err = p.pc.SetLocalDescription(answer); err != nil {
			return err
		}
		p.enqueue(Signal{Type: "answer", SDP: answer.SDP})
		return nil
	case "answer":
		return p.remoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: s.SDP})
	default:
		return errors.New("unsupported WebRTC signaling message")
	}
}
func (p *peer) remoteDescription(s pion.SessionDescription) error {
	if s.SDP == "" {
		return errors.New("SDP is required")
	}
	if err := p.pc.SetRemoteDescription(s); err != nil {
		return err
	}
	p.remote = true
	p.mu.Lock()
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()
	for _, c := range pending {
		if err := p.pc.AddICECandidate(c); err != nil {
			return err
		}
	}
	return nil
}
