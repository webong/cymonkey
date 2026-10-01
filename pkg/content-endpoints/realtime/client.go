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

	"blockade"
	"github.com/gorilla/websocket"
)

type Config struct {
	URL          string
	Model        string
	Voice        string
	Instructions string
	Tools        []blockade.ToolDefinition
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
	probe := blockade.GenerationRequest{Model: config.Model, Messages: []blockade.Message{blockade.TextMessage("user", "validate")}, Tools: config.Tools}
	if err := blockade.ValidateGenerationRequest(probe); err != nil {
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
	if text == "" {
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
	if id == "" || !json.Valid(output) {
		return errors.New("tool result requires a call ID and valid JSON")
	}
	return s.send(ctx, map[string]any{"type": "conversation.item.create", "item": map[string]any{"type": "function_call_output", "call_id": id, "output": string(output)}})
}
func (s *Session) Receive(ctx context.Context) (blockade.RealtimeEvent, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	if err := ctx.Err(); err != nil {
		return blockade.RealtimeEvent{}, err
	}
	deadline, _ := ctx.Deadline()
	if err := s.conn.SetReadDeadline(deadline); err != nil {
		return blockade.RealtimeEvent{}, err
	}
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	_, raw, err := s.conn.ReadMessage()
	if err != nil {
		if ctx.Err() != nil {
			return blockade.RealtimeEvent{}, ctx.Err()
		}
		return blockade.RealtimeEvent{}, err
	}
	var wire struct {
		Type      string `json:"type"`
		Delta     string `json:"delta"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if err = json.Unmarshal(raw, &wire); err != nil {
		return blockade.RealtimeEvent{}, err
	}
	event := blockade.RealtimeEvent{Kind: wire.Type, Raw: raw}
	switch wire.Type {
	case "response.output_text.delta", "response.output_audio_transcript.delta":
		event.Text = wire.Delta
	case "response.output_audio.delta":
		event.Audio, err = base64.StdEncoding.DecodeString(wire.Delta)
		if err != nil {
			return blockade.RealtimeEvent{}, err
		}
	case "response.function_call_arguments.done":
		var args map[string]json.RawMessage
		if wire.CallID == "" || wire.Name == "" || json.Unmarshal([]byte(wire.Arguments), &args) != nil || args == nil {
			return blockade.RealtimeEvent{}, errors.New("invalid realtime tool proposal")
		}
		event.Call = &blockade.ToolCall{ID: wire.CallID, Name: wire.Name, Arguments: json.RawMessage(wire.Arguments)}
	case "error":
		return event, errors.New("realtime provider returned an error event")
	}
	return event, nil
}
func (s *Session) Close() error { s.once.Do(func() { s.closeErr = s.conn.Close() }); return s.closeErr }

var _ blockade.RealtimeSession = (*Session)(nil)
