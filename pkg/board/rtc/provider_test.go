package rtc

import (
	"devicekit"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	pion "github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"io"
	"sync"
	"testing"
	"time"
)

func localAPI() *pion.API {
	var s pion.SettingEngine
	s.SetIncludeLoopbackCandidate(true)
	s.SetNetworkTypes([]pion.NetworkType{pion.NetworkTypeUDP4})
	return pion.NewAPI(pion.WithSettingEngine(s))
}

type testSamples struct {
	closed chan struct{}
	once   sync.Once
	data   []byte
}

func (s *testSamples) ReadSample(ctx context.Context) (media.Sample, error) {
	select {
	case <-ctx.Done():
		return media.Sample{}, ctx.Err()
	case <-s.closed:
		return media.Sample{}, io.EOF
	case <-time.After(20 * time.Millisecond):
		data := s.data
		if data == nil {
			data = make([]byte, 160)
		}
		return media.Sample{Data: data, Duration: 20 * time.Millisecond}, nil
	}
}
func (s *testSamples) Close() error { s.once.Do(func() { close(s.closed) }); return nil }

type testPlayback struct{ received chan []byte }

func (w testPlayback) Write(p []byte) (int, error) {
	select {
	case w.received <- append([]byte(nil), p...):
	default:
	}
	return len(p), nil
}
func (testPlayback) Close() error { return nil }
func TestRealPeersMediaInputAndRestart(t *testing.T) {
	for _, granted := range []bool{true, false} {
		name := "input-granted"
		if !granted {
			name = "view-only"
		}
		t.Run(name, func(t *testing.T) { runRealPeer(t, granted) })
	}
}
func runRealPeer(t *testing.T, inputGranted bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	input := make(chan devicekit.Action, 4)
	played := make(chan []byte, 8)
	received := make(chan struct{}, 16)
	videoReceived := make(chan struct{}, 16)
	fixture, _ := base64.StdEncoding.DecodeString("REtJRgAAIABWUDgwEAAQAB4AAAABAAAAAQAAAAAAAAA4AAAAAAAAAAAAAACwAgCdASoQABAAAEcIhYWIhYSIAgICdaoD+AIM/SgA/v9NEv/8WFfxYV/FhX/xYV/8/M7txfzmAA==")
	codec := pion.RTPCodecCapability{MimeType: pion.MimeTypePCMU, ClockRate: 8000, Channels: 1}
	p, err := New(Options{ID: "rtc", API: localAPI(), TTL: time.Minute, Video: &Source{Codec: pion.RTPCodecCapability{MimeType: pion.MimeTypeVP8, ClockRate: 90000}, Open: func(context.Context) (Samples, error) {
		return &testSamples{closed: make(chan struct{}), data: fixture[44:]}, nil
	}}, Audio: &Source{Codec: codec, Open: func(context.Context) (Samples, error) { return &testSamples{closed: make(chan struct{})}, nil }}, Playback: &Playback{Codec: codec, Open: func(context.Context) (io.WriteCloser, error) { return testPlayback{played}, nil }}, InputCapabilities: []devicekit.Capability{devicekit.KeyboardPress}, Input: func(_ context.Context, a devicekit.Action) error { input <- a; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := devicekit.NewRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	caps := []devicekit.Capability{devicekit.WebRTCSignal, devicekit.WebRTCPoll, devicekit.AudioCapture, devicekit.DisplayVideo}
	if inputGranted {
		caps = append(caps, devicekit.KeyboardPress, devicekit.AudioPlayback)
	}
	c, err := registry.Open(ctx, devicekit.OpenRequest{ProviderID: "rtc", DeviceID: "peer", Grant: devicekit.Grant{Capabilities: caps}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	remote, err := localAPI().NewPeerConnection(pion.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	remote.OnTrack(func(track *pion.TrackRemote, _ *pion.RTPReceiver) {
		go func() {
			for {
				if _, _, err := track.ReadRTP(); err != nil {
					return
				}
				select {
				case func() chan struct{} {
					if track.Kind() == pion.RTPCodecTypeVideo {
						return videoReceived
					}
					return received
				}() <- struct{}{}:
				default:
				}
			}
		}()
	})
	if _, err = remote.AddTransceiverFromKind(pion.RTPCodecTypeAudio, pion.RTPTransceiverInit{Direction: pion.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	if _, err = remote.AddTransceiverFromKind(pion.RTPCodecTypeVideo, pion.RTPTransceiverInit{Direction: pion.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	outgoing, err := pion.NewTrackLocalStaticSample(codec, "speaker", "test")
	if err != nil {
		t.Fatal(err)
	}
	if inputGranted {
		if _, err = remote.AddTransceiverFromTrack(outgoing, pion.RTPTransceiverInit{Direction: pion.RTPTransceiverDirectionSendonly}); err != nil {
			t.Fatal(err)
		}
	}
	dc, err := remote.CreateDataChannel("board.input", nil)
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan struct{})
	dc.OnOpen(func() { close(opened) })
	invoke := func(s Signal) {
		b, _ := json.Marshal(s)
		if _, err := c.Invoke(ctx, devicekit.Action{Capability: devicekit.WebRTCSignal, Input: b}); err != nil {
			t.Fatal(err)
		}
	}
	negotiate := func(restart bool) {
		offer, err := remote.CreateOffer(&pion.OfferOptions{ICERestart: restart})
		if err != nil {
			t.Fatal(err)
		}
		gather := pion.GatheringCompletePromise(remote)
		if err = remote.SetLocalDescription(offer); err != nil {
			t.Fatal(err)
		}
		select {
		case <-gather:
		case <-ctx.Done():
			t.Fatal("ICE gathering timeout")
		}
		invoke(Signal{Type: "offer", SDP: remote.LocalDescription().SDP})
		answered := false
		var candidates []pion.ICECandidateInit
		for {
			result, err := c.Invoke(ctx, devicekit.Action{Capability: devicekit.WebRTCPoll})
			if err != nil {
				t.Fatal(err)
			}
			var signals []Signal
			if err = json.Unmarshal(result.Output, &signals); err != nil {
				t.Fatal(err)
			}
			for _, s := range signals {
				switch s.Type {
				case "answer":
					if err = remote.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: s.SDP}); err != nil {
						t.Fatal(err)
					}
					answered = true
				case "ice-candidate":
					candidates = append(candidates, *s.Candidate)
				}
			}
			if answered {
				for _, candidate := range candidates {
					if err := remote.AddICECandidate(candidate); err != nil {
						t.Fatal(err)
					}
				}
				candidates = nil
				if remote.ConnectionState() == pion.PeerConnectionStateConnected {
					break
				}
			}
			select {
			case <-time.After(20 * time.Millisecond):
			case <-ctx.Done():
				t.Fatal("connect timeout")
			}
		}
	}
	negotiate(false)
	select {
	case <-opened:
	case <-ctx.Done():
		t.Fatal("data channel did not open")
	}
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = outgoing.WriteSample(media.Sample{Data: make([]byte, 160), Duration: 20 * time.Millisecond})
			}
		}
	}()
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("outbound audio not delivered")
	}
	select {
	case <-videoReceived:
	case <-ctx.Done():
		t.Fatal("VP8 not delivered")
	}
	if inputGranted {
		select {
		case <-played:
		case <-ctx.Done():
			t.Fatal("inbound audio not delivered")
		}
	} else {
		if _, err = c.Invoke(ctx, devicekit.Action{Capability: devicekit.AudioPlayback}); !errors.Is(err, devicekit.ErrNotGranted) {
			t.Fatal("view grant allowed playback")
		}
		select {
		case <-played:
			t.Fatal("ungranted playback delivered")
		default:
		}
	}
	if err = dc.SendText(`{"capability":"keyboard.press","input":{"key":"a"}}`); err != nil {
		t.Fatal(err)
	}
	if inputGranted {
		select {
		case <-input:
		case <-ctx.Done():
			t.Fatal("input not forwarded")
		}
	} else {
		select {
		case <-input:
			t.Fatal("view-only grant forwarded input")
		case <-time.After(150 * time.Millisecond):
		}
	}
	if _, err = c.Invoke(ctx, devicekit.Action{Capability: devicekit.KeyboardType, Input: []byte(`{"text":"denied"}`)}); !errors.Is(err, devicekit.ErrNotGranted) {
		t.Fatalf("ungranted text input: %v", err)
	}
	negotiate(true)
	for len(received) > 0 {
		<-received
	}
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("media did not survive ICE restart")
	}
	invoke(Signal{Type: "bye"})
	if _, err = c.Invoke(ctx, devicekit.Action{Capability: devicekit.WebRTCSignal, Input: []byte(`{"type":"heartbeat"}`)}); !errors.Is(err, devicekit.ErrClosed) {
		t.Fatalf("peer remained open: %v", err)
	}
}
func TestPeerExpiryAndQueueBounds(t *testing.T) {
	p, err := openPeer(context.Background(), Options{API: localAPI(), TTL: 20 * time.Millisecond}, map[devicekit.Capability]bool{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	<-p.ctx.Done()
	if err := p.signal(context.Background(), Signal{Type: "heartbeat"}); !errors.Is(err, devicekit.ErrClosed) {
		t.Fatal(err)
	}
	q, err := openPeer(context.Background(), Options{API: localAPI(), TTL: time.Minute}, map[devicekit.Capability]bool{})
	if err != nil {
		t.Fatal(err)
	}
	defer q.close()
	for i := 0; i < 129; i++ {
		q.enqueue(Signal{Type: "heartbeat"})
	}
	select {
	case <-q.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("signaling overflow did not close peer")
	}
}

func TestNeutralEnvelopeAndIVFBounds(t *testing.T) {
	m := Message{Type: Offer, SessionID: "viewer-one", From: "viewer", Payload: json.RawMessage(`{"sdp":"offer"}`)}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	m.Type = "invalid"
	if err := m.Validate(); err == nil {
		t.Fatal("invalid type accepted")
	}
	m.Type = Offer
	if _, err := (Forwarder{}).Forward(context.Background(), m); !errors.Is(err, devicekit.ErrNotGranted) {
		t.Fatal(err)
	}
	fixture, _ := base64.StdEncoding.DecodeString("REtJRgAAIABWUDgwEAAQAB4AAAABAAAAAQAAAAAAAAA4AAAAAAAAAAAAAACwAgCdASoQABAAAEcIhYWIhYSIAgICdaoD+AIM/SgA/v9NEv/8WFfxYV/FhX/xYV/8/M7txfzmAA==")
	reader := &samples{ch: sampleChannel{bytes.NewReader(fixture[32:])}, video: true, duration: time.Second / 30}
	sample, err := reader.ReadSample(context.Background())
	if err != nil || !bytes.Equal(sample.Data, fixture[44:]) {
		t.Fatalf("IVF sample %v", err)
	}
	invalid := append([]byte{255, 255, 255, 255}, make([]byte, 8)...)
	reader.ch = sampleChannel{bytes.NewReader(invalid)}
	if _, err = reader.ReadSample(context.Background()); !errors.Is(err, devicekit.ErrLimit) {
		t.Fatal(err)
	}
}

type sampleChannel struct{ *bytes.Reader }

func (sampleChannel) Close() error              { return nil }
func (sampleChannel) Write([]byte) (int, error) { return 0, devicekit.ErrNotGranted }
