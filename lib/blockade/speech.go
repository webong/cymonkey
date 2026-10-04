package blockade

import (
	"context"
	"io"
	"time"
)

type TranscriptionRequest struct {
	Model      string
	Audio      []byte
	MediaType  string
	Language   string
	Prompt     string
	Metadata   bool
	Timestamps []string // optional word/segment timing; adapters may reject
}
type TranscriptionResponse struct {
	Text            string                 `json:"text"`
	Language        string                 `json:"language,omitempty"`
	DurationSeconds *float64               `json:"durationSeconds,omitempty"`
	Segments        []TranscriptionSegment `json:"segments,omitempty"`
	Words           []TranscriptionWord    `json:"words,omitempty"`
}
type TranscriptionSegment struct {
	ID    int     `json:"id"`
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}
type TranscriptionWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}
type Transcriber interface {
	Transcribe(context.Context, TranscriptionRequest) (TranscriptionResponse, error)
}

type SpeechRequest struct {
	Model        string `json:"model"`
	Text         string `json:"text"`
	Voice        string `json:"voice"`
	Format       string `json:"format"` // mp3, opus, aac, flac, wav, pcm
	Instructions string `json:"instructions,omitempty"`
}

// SpeechStream is a forward-only audio response. The caller owns playback
// and must close the stream, including when cancellation or playback fails.
type SpeechStream struct {
	Audio     io.ReadCloser
	MediaType string
}
type SpeechSynthesizer interface {
	Synthesize(context.Context, SpeechRequest) (SpeechStream, error)
}

// RealtimeSession is an explicit duplex model connection. It never opens
// devices or executes proposed tools. The caller owns turns and input/output.
type RealtimeSession interface {
	SendText(context.Context, string) error
	SendAudio(context.Context, []byte) error
	CommitAudio(context.Context) error
	Respond(context.Context) error
	Interrupt(context.Context) error
	SendToolResult(context.Context, string, []byte) error
	Receive(context.Context) (RealtimeEvent, error)
	Close() error
}
type RealtimeEvent struct {
	Kind       string      `json:"kind"`
	NativeType string      `json:"nativeType,omitempty"`
	ID         string      `json:"id,omitempty"`
	ItemID     string      `json:"itemId,omitempty"`
	ResponseID string      `json:"responseId,omitempty"`
	Status     string      `json:"status,omitempty"`
	Text       string      `json:"text,omitempty"`
	Audio      []byte      `json:"audio,omitempty"`
	Call       *ToolCall   `json:"call,omitempty"`
	Usage      *TokenUsage `json:"usage,omitempty"`
	OccurredAt time.Time   `json:"occurredAt"`
	Raw        []byte      `json:"raw,omitempty"`
}

// RealtimeAudioControl is optional. Canceling model output and truncating the
// conversation to audio actually played are separate, caller-selected actions.
type RealtimeAudioControl interface {
	ClearInputAudio(context.Context) error
	TruncateAudio(context.Context, string, int, int) error // item ID, content index, audio milliseconds
}

// RealtimeState is a pure event reducer. It owns no connection, microphone,
// playback queue, transcript store, tool registry or background task.
type RealtimeState struct {
	Connection     string    `json:"connection"`
	ResponseID     string    `json:"responseId,omitempty"`
	ResponseStatus string    `json:"responseStatus,omitempty"`
	LastEventAt    time.Time `json:"lastEventAt"`
}

func (s RealtimeState) Apply(event RealtimeEvent) RealtimeState {
	s.LastEventAt = event.OccurredAt
	switch event.Kind {
	case "session.ready":
		s.Connection = "ready"
	case "session.closed":
		s.Connection = "closed"
	case "error":
		s.Connection = "error"
	case "response.started":
		s.ResponseID = event.ResponseID
		s.ResponseStatus = "responding"
	case "response.completed", "response.interrupted":
		s.ResponseID = event.ResponseID
		s.ResponseStatus = event.Status
	}
	return s
}
