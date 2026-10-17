// Package realtime implements the Realtime WebSocket protocol in an optional
// provider package. No endpoint, model, credential source, or device is implicit.
package realtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"kit/generationkit"
)

type Config struct {
	URL          string
	Model        string
	Voice        string
	Instructions string
	Tools        []generationkit.ToolDefinition
	APIKey       func(context.Context) (string, error)
	Dialer       *websocket.Dialer
}

// Session owns only the model WebSocket. SendAudio accepts mono PCM16 at
// 24 kHz. Input turns and model responses advance only through explicit calls.
type Session struct {
	conn            *websocket.Conn
	readMu, writeMu sync.Mutex
	once            sync.Once
	closeErr        error
}

func Open(ctx context.Context, config Config) (*Session, error) {
	u, err := url.Parse(config.URL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" || u.User != nil || u.Fragment != "" || config.Model == "" {
		return nil, errors.New("realtime needs an explicit WebSocket endpoint and model")
	}
	probe := generationkit.GenerationRequest{Model: config.Model, Messages: []generationkit.Message{generationkit.TextMessage("user", "validate")}, Tools: config.Tools}
	if err := generationkit.ValidateGenerationRequest(probe); err != nil {
		return nil, err
	}
	query := u.Query()
	query.Set("model", config.Model)
	u.RawQuery = query.Encode()
	headers := http.Header{}
	if config.APIKey != nil {
		key, err := config.APIKey(ctx)
		if err != nil {
			return nil, err
		}
		headers.Set("Authorization", "Bearer "+key)
	}
	dialer := config.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, resp, err := dialer.DialContext(ctx, u.String(), headers)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(16 << 20)
	session := &Session{conn: conn}
	tools := make([]any, 0, len(config.Tools))
	for _, tool := range config.Tools {
		tools = append(tools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.InputSchema})
	}
	modalities := []string{"text"}
	audio := map[string]any{"input": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 24000}, "turn_detection": nil}}
	if config.Voice != "" {
		modalities = []string{"audio"}
		audio["output"] = map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 24000}, "voice": config.Voice}
	}
	update := map[string]any{"type": "session.update", "session": map[string]any{"type": "realtime", "model": config.Model, "instructions": config.Instructions, "output_modalities": modalities, "audio": audio, "tools": tools}}
	if err := session.send(ctx, update); err != nil {
		session.Close()
		return nil, err
	}
	return session, nil
}

func (s *Session) send(ctx context.Context, event any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	if err := s.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	err := s.conn.WriteJSON(event)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func (s *Session) SendText(ctx context.Context, text string) error {
	if text == "" || len(text) > 1<<20 {
		return errors.New("realtime text cannot be empty")
	}
	return s.send(ctx, map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}}})
}
func (s *Session) SendAudio(ctx context.Context, audio []byte) error {
	if len(audio) == 0 || len(audio)%2 != 0 || len(audio) > 4<<20 {
		return errors.New("audio chunk must contain at most 4 MiB of PCM16 frames")
	}
	return s.send(ctx, map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(audio)})
}
func (s *Session) CommitAudio(ctx context.Context) error {
	return s.send(ctx, map[string]any{"type": "input_audio_buffer.commit"})
}
func (s *Session) Respond(ctx context.Context) error {
	return s.send(ctx, map[string]any{"type": "response.create"})
}
func (s *Session) Interrupt(ctx context.Context) error {
	return s.send(ctx, map[string]any{"type": "response.cancel"})
}
func (s *Session) SendToolResult(ctx context.Context, id string, output []byte) error {
	if id == "" || len(id) > 256 || len(output) > 1<<20 || !json.Valid(output) {
		return errors.New("tool result requires a call ID and valid JSON")
	}
	return s.send(ctx, map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "function_call_output", "call_id": id, "output": string(output)}})
}
func (s *Session) Receive(ctx context.Context) (generationkit.RealtimeEvent, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	if err := ctx.Err(); err != nil {
		return generationkit.RealtimeEvent{}, err
	}
	deadline, _ := ctx.Deadline()
	if err := s.conn.SetReadDeadline(deadline); err != nil {
		return generationkit.RealtimeEvent{}, err
	}
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	_, raw, err := s.conn.ReadMessage()
	if err != nil {
		if ctx.Err() != nil {
			return generationkit.RealtimeEvent{}, ctx.Err()
		}
		return generationkit.RealtimeEvent{}, err
	}
	var wire struct {
		Type       string `json:"type"`
		Delta      string `json:"delta"`
		CallID     string `json:"call_id"`
		Name       string `json:"name"`
		Arguments  string `json:"arguments"`
		ID         string `json:"event_id"`
		ItemID     string `json:"item_id"`
		ResponseID string `json:"response_id"`
		Transcript string `json:"transcript"`
		Response   struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Usage  struct {
				Input  int64 `json:"input_tokens"`
				Output int64 `json:"output_tokens"`
				Total  int64 `json:"total_tokens"`
			} `json:"usage"`
		} `json:"response"`
	}
	if err = json.Unmarshal(raw, &wire); err != nil {
		return generationkit.RealtimeEvent{}, err
	}
	event := generationkit.RealtimeEvent{Kind: wire.Type, NativeType: wire.Type, ID: wire.ID, ItemID: wire.ItemID, ResponseID: wire.ResponseID, OccurredAt: time.Now().UTC(), Raw: raw}
	switch wire.Type {
	case "session.created", "session.updated":
		event.Kind = "session.ready"
	case "response.created":
		event.Kind = "response.started"
		event.ResponseID = wire.Response.ID
		event.Status = wire.Response.Status
	case "response.done":
		event.Kind = "response.completed"
		event.ResponseID = wire.Response.ID
		event.Status = wire.Response.Status
		if wire.Response.Status == "cancelled" {
			event.Kind = "response.interrupted"
		}
		event.Usage = &generationkit.TokenUsage{Input: wire.Response.Usage.Input, Output: wire.Response.Usage.Output, Total: wire.Response.Usage.Total}
	case "input_audio_buffer.committed":
		event.Kind = "audio.committed"
	case "conversation.item.input_audio_transcription.completed":
		event.Kind = "transcription.completed"
		event.Text = wire.Transcript
	case "conversation.item.input_audio_transcription.delta":
		event.Kind = "transcription.delta"
		event.Text = wire.Delta
	case "response.output_text.delta":
		event.Kind = "text.delta"
		event.Text = wire.Delta
	case "response.output_audio_transcript.delta":
		event.Kind = "transcript.delta"
		event.Text = wire.Delta
	case "response.output_audio.delta":
		event.Kind = "audio.delta"
		event.Audio, err = base64.StdEncoding.DecodeString(wire.Delta)
		if err != nil || len(event.Audio)%2 != 0 {
			return generationkit.RealtimeEvent{}, errors.New("invalid realtime PCM16 audio delta")
		}
	case "response.function_call_arguments.done":
		event.Kind = "tool.completed"
		var args map[string]json.RawMessage
		if wire.CallID == "" || wire.Name == "" || json.Unmarshal([]byte(wire.Arguments), &args) != nil || args == nil {
			return generationkit.RealtimeEvent{}, errors.New("invalid realtime tool proposal")
		}
		event.Call = &generationkit.ToolCall{ID: wire.CallID, Name: wire.Name, Arguments: json.RawMessage(wire.Arguments)}
	case "error":
		return event, errors.New("realtime provider returned an error event")
	}
	return event, nil
}
func (s *Session) Close() error { s.once.Do(func() { s.closeErr = s.conn.Close() }); return s.closeErr }

var _ generationkit.RealtimeSession = (*Session)(nil)

func (s *Session) ClearInputAudio(ctx context.Context) error {
	return s.send(ctx, map[string]any{"type": "input_audio_buffer.clear"})
}
func (s *Session) TruncateAudio(ctx context.Context, itemID string, contentIndex, audioMilliseconds int) error {
	if itemID == "" || contentIndex < 0 || audioMilliseconds < 0 || audioMilliseconds > 3600000 {
		return errors.New("invalid played-audio boundary")
	}
	return s.send(ctx, map[string]any{"type": "conversation.item.truncate", "item_id": itemID, "content_index": contentIndex, "audio_end_ms": audioMilliseconds})
}

var _ generationkit.RealtimeAudioControl = (*Session)(nil)
