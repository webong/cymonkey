package blockade

import (
	"context"
	"io"
)

type TranscriptionRequest struct {
	Model     string
	Audio     []byte
	MediaType string
	Language  string
	Prompt    string
}
type TranscriptionResponse struct {
	Text string `json:"text"`
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
	Kind  string
	Text  string
	Audio []byte
	Call  *ToolCall
	Raw   []byte
}
