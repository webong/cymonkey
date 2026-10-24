package mobiletarget

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"net"
	"testing"
	"time"

	"jangolova/devicekit"
	"jangolova/mobilekit"

	"github.com/gorilla/websocket"
	pion "github.com/pion/webrtc/v4"
)

func hasLANAddress() bool {
	addresses, _ := net.InterfaceAddrs()
	for _, address := range addresses {
		if network, ok := address.(*net.IPNet); ok && network.IP.To4() != nil && !network.IP.IsLoopback() {
			return true
		}
	}
	return false
}

func testFrame(t *testing.T, shade uint8) []byte {
	t.Helper()
	picture := image.NewGray(image.Rect(0, 0, 64, 48))
	for i := range picture.Pix {
		picture.Pix[i] = shade + uint8(i)
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, picture, nil); err != nil {
		t.Fatal(err)
	}
	return append([]byte{mobilekit.KindVideo}, buffer.Bytes()...)
}

// viewer opens a device session on the target and negotiates a WebRTC peer
// that receives video, the way a browser viewer would.
func connectViewer(t *testing.T, server *Server, videoReceived chan struct{}) (*pion.PeerConnection, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	call := func(operation string, params any) json.RawMessage {
		raw, _ := json.Marshal(params)
		result, err := server.Call(ctx, "mobile.target."+operation, raw)
		if err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
		encoded, _ := json.Marshal(result)
		return encoded
	}
	call("open", map[string]any{"deviceId": "peer", "grant": devicekit.Grant{Capabilities: []devicekit.Capability{devicekit.WebRTCSignal, devicekit.WebRTCPoll, devicekit.DisplayVideo}}})
	invoke := func(capability devicekit.Capability, input any) json.RawMessage {
		action := devicekit.Action{Capability: capability}
		if input != nil {
			action.Input, _ = json.Marshal(input)
		}
		var result devicekit.Result
		if err := json.Unmarshal(call("invoke", action), &result); err != nil {
			t.Fatal(err)
		}
		return result.Output
	}
	remote, err := pion.NewAPI().NewPeerConnection(pion.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	remote.OnTrack(func(track *pion.TrackRemote, _ *pion.RTPReceiver) {
		go func() {
			for {
				if _, _, err := track.ReadRTP(); err != nil {
					return
				}
				select {
				case videoReceived <- struct{}{}:
				default:
				}
			}
		}()
	})
	if _, err := remote.AddTransceiverFromKind(pion.RTPCodecTypeVideo, pion.RTPTransceiverInit{Direction: pion.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	offer, err := remote.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := pion.GatheringCompletePromise(remote)
	if err := remote.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	invoke(devicekit.WebRTCSignal, map[string]string{"type": "offer", "sdp": remote.LocalDescription().SDP})
	answered := false
	var candidates []pion.ICECandidateInit
	for !answered || remote.ConnectionState() != pion.PeerConnectionStateConnected {
		var signals []struct {
			Type      string                 `json:"type"`
			SDP       string                 `json:"sdp"`
			Candidate *pion.ICECandidateInit `json:"candidate"`
		}
		if err := json.Unmarshal(invoke(devicekit.WebRTCPoll, nil), &signals); err != nil {
			t.Fatal(err)
		}
		for _, signal := range signals {
			switch signal.Type {
			case "answer":
				if err := remote.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: signal.SDP}); err != nil {
					t.Fatal(err)
				}
				answered = true
			case "ice-candidate":
				candidates = append(candidates, *signal.Candidate)
			}
		}
		if answered {
			for _, candidate := range candidates {
				if err := remote.AddICECandidate(candidate); err != nil {
					t.Fatal(err)
				}
			}
			candidates = nil
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("the viewer did not connect")
		}
	}
	return remote, func() {
		remote.Close()
		call("close", nil)
		cancel()
	}
}

// TestViewerReceivesTheAppsVideoOverWebRTC runs the whole data path in the
// guest: the app's JPEG frames, ffmpeg, the VP8 track, and a real peer that
// reads RTP; then a second viewer attaches to the same target.
func TestViewerReceivesTheAppsVideoOverWebRTC(t *testing.T) {
	if !hasLANAddress() {
		t.Skip("no non-loopback IPv4 address for ICE host candidates")
	}
	server, result := startServer(t, mobilekit.Media{Video: true})
	app, _, err := dial(t, result, result.Endpoint.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for shade := uint8(0); ; shade += 3 {
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
			}
			if app.WriteMessage(websocket.BinaryMessage, testFrame(t, shade)) != nil {
				return
			}
		}
	}()
	for viewer := 1; viewer <= 2; viewer++ {
		received := make(chan struct{}, 4)
		_, closeViewer := connectViewer(t, server, received)
		select {
		case <-received:
		case <-time.After(15 * time.Second):
			t.Fatalf("viewer %d received no video", viewer)
		}
		closeViewer()
	}
}
