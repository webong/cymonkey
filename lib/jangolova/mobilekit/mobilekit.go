// Package mobilekit is the contract between the host and a mobile-target
// package. A mobile target is a native app that streams its screen and audio
// to Cymonkey and plays audio sent back, with a viewer watching over WebRTC.
// The package runs one long-lived process per target. It serves mobile.target
// v1: start and stop, plus the device operations (list, open, invoke,
// stream.*, close, events) of the viewer half, under the same contract name so
// that no device.provider discovery launches it.
package mobilekit

import (
	"bytes"
	"errors"
	"image/jpeg"

	"github.com/webong/ext/pkg/plugin-go/contract/endpoint"
)

const (
	Contract = "mobile.target"
	Version  = "v1"

	// Codes of the RemoteError a package returns for a start it refuses.
	CodeConverterUnavailable = "converter_unavailable"
	CodeInvalidTarget        = "invalid_target"
)

// ControlOperations are the operations that belong to the target itself; the
// device operations of the viewer half are listed by the device plugin.
var ControlOperations = []string{"start", "stop"}

// Wire kinds of a frame from or to the app: one kind byte, then the payload.
// PCM is signed little-endian 16-bit stereo at 48 kHz.
const (
	KindVideo      byte = 1 // JPEG
	KindAudio      byte = 2 // PCM, system audio
	KindMicrophone byte = 3 // PCM, microphone
	KindPlayback   byte = 4 // PCM, to the app
)

// Bounds on what the app may send.
const (
	MaxVideoBytes = 4 << 20
	MaxAudioBytes = 3840
	MaxFrameBytes = MaxVideoBytes + 1
	// MaxTotalBytes ends an attachment that has sent this much.
	MaxTotalBytes = 1 << 30
	maxDimension  = 4096
	maxPixels     = 8 << 20
)

var (
	ErrFrame      = errors.New("mobile target frame is not valid")
	ErrNotGranted = errors.New("mobile target media kind is not granted")
)

// Media is what the app may send and receive.
type Media struct {
	Video      bool `json:"video"`
	Audio      bool `json:"audio"`
	Microphone bool `json:"microphone"`
	Playback   bool `json:"playback"`
}

// Any reports whether at least one kind is granted.
func (m Media) Any() bool { return m.Video || m.Audio || m.Microphone || m.Playback }

// CheckFrame validates one frame from the app: a granted inbound kind, the size
// bound of that kind and, for video, the dimensions of the JPEG.
func CheckFrame(media Media, frame []byte) error {
	if len(frame) < 2 {
		return ErrFrame
	}
	payload := frame[1:]
	switch frame[0] {
	case KindVideo:
		if !media.Video {
			return ErrNotGranted
		}
		if len(payload) > MaxVideoBytes {
			return ErrFrame
		}
		image, err := jpeg.DecodeConfig(bytes.NewReader(payload))
		if err != nil || image.Width <= 0 || image.Height <= 0 || image.Width > maxDimension || image.Height > maxDimension || image.Width*image.Height > maxPixels {
			return ErrFrame
		}
	case KindAudio, KindMicrophone:
		if frame[0] == KindAudio && !media.Audio || frame[0] == KindMicrophone && !media.Microphone {
			return ErrNotGranted
		}
		if len(payload) > MaxAudioBytes || len(payload)%4 != 0 {
			return ErrFrame
		}
	default:
		return ErrNotGranted
	}
	return nil
}

// ICEServer is a STUN or TURN server, as plain data.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// StartRequest is what mobile.target.start takes. ICE credentials are secrets:
// neither side may put them in an error or a log.
type StartRequest struct {
	ID         string      `json:"id"`
	Name       string      `json:"name,omitempty"`
	ICEServers []ICEServer `json:"iceServers,omitempty"`
	RelayOnly  bool        `json:"relayOnly,omitempty"`
	Media      Media       `json:"media"`
	TTLSeconds int         `json:"ttlSeconds"`
}

// StartResult carries the declared loopback endpoint on which the app's frames
// are accepted (a ws endpoint with a bearer token; ext keeps it secret).
type StartResult struct {
	Endpoint endpoint.Endpoint `json:"endpoint"`
}
