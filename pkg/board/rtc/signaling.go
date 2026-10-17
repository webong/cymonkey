package rtc

import (
	"context"
	"encoding/json"
	"errors"
	pion "github.com/pion/webrtc/v4"
	"kit/devicekit"
	"strings"
	"time"
)

// Message is the neutral signaling envelope extracted from Xallet's display
// contract. The host authenticates principals and binds this envelope to a
// Connection; routing identifiers never select a device by themselves.
type MessageType string

const (
	Offer     MessageType = "offer"
	Answer    MessageType = "answer"
	ICE       MessageType = "ice-candidate"
	Bye       MessageType = "bye"
	Heartbeat MessageType = "heartbeat"
	Restart   MessageType = "restart"
)

type Message struct {
	Type      MessageType     `json:"type"`
	SessionID string          `json:"sessionId"`
	From      string          `json:"from"`
	To        string          `json:"to,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	SentAt    time.Time       `json:"sentAt"`
}

func (m Message) Validate() error {
	if strings.TrimSpace(m.SessionID) == "" || strings.TrimSpace(m.From) == "" || len(m.SessionID) > 256 || len(m.From) > 256 || len(m.To) > 256 {
		return errors.New("signaling requires bounded session and sender identifiers")
	}
	switch m.Type {
	case Offer, Answer, ICE, Bye, Heartbeat, Restart:
	default:
		return errors.New("unsupported signaling message type")
	}
	if len(m.Payload) > 512<<10 {
		return devicekit.ErrLimit
	}
	if len(m.Payload) > 0 && !json.Valid(m.Payload) {
		return errors.New("invalid signaling payload")
	}
	return nil
}

// Forwarder replaces host-specific forwarding code with one authorized binding.
// The host must authenticate Remote before calling Forward or Poll.
type Forwarder struct {
	SessionID, Local, Remote string
	Connection               *devicekit.Connection
}

func (f Forwarder) valid() bool {
	return f.Connection != nil && f.SessionID != "" && f.Local != "" && f.Remote != ""
}
func (f Forwarder) Forward(ctx context.Context, m Message) ([]Message, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if !f.valid() || m.SessionID != f.SessionID || m.From != f.Remote || m.To != "" && m.To != f.Local {
		return nil, devicekit.ErrNotGranted
	}
	s := Signal{Type: string(m.Type)}
	switch m.Type {
	case Offer, Answer:
		var payload struct {
			SDP string `json:"sdp"`
		}
		if err := json.Unmarshal(m.Payload, &payload); err != nil {
			return nil, err
		}
		if payload.SDP == "" {
			return nil, errors.New("SDP is required")
		}
		s.SDP = payload.SDP
	case ICE:
		var candidate pion.ICECandidateInit
		if err := json.Unmarshal(m.Payload, &candidate); err != nil {
			return nil, err
		}
		s.Candidate = &candidate
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if _, err = f.Connection.Invoke(ctx, devicekit.Action{Capability: devicekit.WebRTCSignal, Input: raw}); err != nil {
		return nil, err
	}
	return f.Poll(ctx)
}
func (f Forwarder) Poll(ctx context.Context) ([]Message, error) {
	if !f.valid() {
		return nil, devicekit.ErrNotGranted
	}
	result, err := f.Connection.Invoke(ctx, devicekit.Action{Capability: devicekit.WebRTCPoll})
	if err != nil {
		return nil, err
	}
	var signals []Signal
	if err = json.Unmarshal(result.Output, &signals); err != nil {
		return nil, err
	}
	messages := make([]Message, 0, len(signals))
	for _, s := range signals {
		m := Message{Type: MessageType(s.Type), SessionID: f.SessionID, From: f.Local, To: f.Remote, SentAt: s.At}
		if s.SDP != "" {
			m.Payload, _ = json.Marshal(map[string]string{"sdp": s.SDP})
		} else if s.Candidate != nil {
			m.Payload, _ = json.Marshal(s.Candidate)
		}
		messages = append(messages, m)
	}
	return messages, nil
}
