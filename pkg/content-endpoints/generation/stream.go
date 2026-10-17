package generation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"kit/generationkit"
)

// Stream performs one request with synchronous lifecycle, delta, usage and
// completed-call events. Callback failures stop delivery and abort transport.
func (c *Client) Stream(ctx context.Context, request generationkit.GenerationRequest, emit func(generationkit.GenerationEvent) error) (generationkit.GenerationResponse, error) {
	if emit == nil {
		return generationkit.GenerationResponse{}, errors.New("stream callback is required")
	}
	if err := ctx.Err(); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	if _, _, err := c.encode(request, true); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	if err := emit(generationkit.GenerationEvent{Kind: "started"}); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	return c.streamRaw(ctx, request, emit)
}

// Stream emits deltas without executing tool calls. Truncated streams fail
// rather than reporting a successful completion with partial tool arguments.
func (c *Client) streamRaw(ctx context.Context, request generationkit.GenerationRequest, emit func(generationkit.GenerationEvent) error) (generationkit.GenerationResponse, error) {
	if emit == nil {
		return generationkit.GenerationResponse{}, errors.New("stream callback is required")
	}
	if c.config.Protocol == Responses {
		return c.streamResponses(ctx, request, emit)
	}
	body, path, err := c.encode(request, true)
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, path, "application/json", bytes.NewReader(body), true)
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	defer resp.Body.Close()
	r := generationkit.GenerationResponse{Model: request.Model, Message: generationkit.Message{Role: "assistant"}}
	calls := map[int]*generationkit.ToolCall{}
	fallback := map[int]json.RawMessage{}
	terminal := false
	var text, reasoning, refusal strings.Builder
	thinking := map[int]*anthropicBlock{}
	blocks := map[int]string{}
	closed := map[int]bool{}
	messageStarted := false
	choiceFinished := false
	err = readSSE(io.LimitReader(resp.Body, c.config.MaxResponseBytes+1), c.config.MaxResponseBytes, func(data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if string(data) == "[DONE]" {
			if c.config.Protocol == OpenAI && r.StopReason != "" {
				terminal = true
			}
			return nil
		}
		if terminal && c.config.Protocol != Gemini {
			return errors.New("stream event follows terminal response")
		}
		switch c.config.Protocol {
		case OpenAI:
			var wire openAIResponse
			if err := json.Unmarshal(data, &wire); err != nil {
				return err
			}
			if hasError(wire.Error) {
				return errors.New("model stream returned a provider error")
			}
			if wire.ID != "" {
				r.ID = wire.ID
			}
			if wire.Model != "" {
				r.Model = wire.Model
			}
			if wire.Usage.Total > 0 {
				r.Usage = generationkit.TokenUsage{Input: wire.Usage.Input, Output: wire.Usage.Output, Total: wire.Usage.Total, CachedInput: wire.Usage.InputDetails.Cached, Reasoning: wire.Usage.OutputDetails.Reasoning}
			}
			for _, choice := range wire.Choices {
				if choiceFinished {
					return errors.New("choice delta follows finish reason")
				}
				if choice.Index != 0 {
					return unsupported("multiple streamed choices")
				}
				if choice.FinishReason != "" {
					choiceFinished = true
					r.StopReason = choice.FinishReason
				}
				delta := choice.Delta.Content
				if choice.Delta.Reasoning != "" {
					reasoning.WriteString(choice.Delta.Reasoning)
					if err := emit(generationkit.GenerationEvent{Kind: "reasoning.delta", Text: choice.Delta.Reasoning}); err != nil {
						return err
					}
				}
				if choice.Delta.Refusal != "" {
					refusal.WriteString(choice.Delta.Refusal)
					r.StopReason = "refusal"
					if err := emit(generationkit.GenerationEvent{Kind: "refusal.delta", Text: choice.Delta.Refusal}); err != nil {
						return err
					}
				}
				if delta != "" {
					text.WriteString(delta)
					if err := emit(generationkit.GenerationEvent{Kind: "text.delta", Text: delta}); err != nil {
						return err
					}
				}
				for _, part := range choice.Delta.ToolCalls {
					if part.Index < 0 || part.Index > 1024 {
						return errors.New("invalid tool call index")
					}
					call := calls[part.Index]
					if call == nil {
						call = &generationkit.ToolCall{}
						calls[part.Index] = call
					}
					if part.ID != "" {
						if call.ID != "" && call.ID != part.ID {
							return errors.New("streamed tool identity changed")
						}
						call.ID = part.ID
					}
					call.Name += part.Function.Name
					call.Arguments = append(call.Arguments, part.Function.Arguments...)
					if err := emit(generationkit.GenerationEvent{Kind: "tool.delta", Index: part.Index, CallID: call.ID, ToolName: part.Function.Name, ArgumentsDelta: part.Function.Arguments}); err != nil {
						return err
					}
				}
			}
		case Anthropic:
			var event struct {
				Type    string            `json:"type"`
				Index   int               `json:"index"`
				Message anthropicResponse `json:"message"`
				Block   anthropicBlock    `json:"content_block"`
				Delta   struct {
					Type       string `json:"type"`
					Text       string `json:"text"`
					JSON       string `json:"partial_json"`
					StopReason string `json:"stop_reason"`
					Thinking   string `json:"thinking"`
					Signature  string `json:"signature"`
				} `json:"delta"`
				Usage struct {
					Input  int64 `json:"input_tokens"`
					Output int64 `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(data, &event); err != nil {
				return err
			}
			switch event.Type {
			case "error":
				return errors.New("model stream returned a provider error")
			case "message_start":
				if messageStarted {
					return errors.New("duplicate message start")
				}
				messageStarted = true
				r.ID = event.Message.ID
				if event.Message.Model != "" {
					r.Model = event.Message.Model
				}
				r.Usage.Input = event.Message.Usage.Input + event.Message.Usage.Cached + event.Message.Usage.CacheWrite
				r.Usage.CachedInput = event.Message.Usage.Cached
			case "content_block_start":
				if !messageStarted || blocks[event.Index] != "" {
					return errors.New("invalid or duplicate block start")
				}
				if event.Index < 0 || event.Index > 1024 {
					return errors.New("invalid content block index")
				}
				blocks[event.Index] = event.Block.Type
				switch event.Block.Type {
				case "tool_use":
					calls[event.Index] = &generationkit.ToolCall{ID: event.Block.ID, Name: event.Block.Name}
					fallback[event.Index] = event.Block.Input
					if err := emit(generationkit.GenerationEvent{Kind: "tool.delta", Index: event.Index, CallID: event.Block.ID, ToolName: event.Block.Name}); err != nil {
						return err
					}
				case "thinking", "redacted_thinking":
					block := event.Block
					thinking[event.Index] = &block
					if block.Thinking != "" {
						if err := emit(generationkit.GenerationEvent{Kind: "reasoning.delta", Index: event.Index, Text: block.Thinking}); err != nil {
							return err
						}
					}
				case "text":
					if event.Block.Text != "" {
						text.WriteString(event.Block.Text)
						if err := emit(generationkit.GenerationEvent{Kind: "text.delta", Text: event.Block.Text}); err != nil {
							return err
						}
					}
				default:
					return unsupported("Anthropic streamed block " + event.Block.Type)
				}
			case "content_block_delta":
				if blocks[event.Index] == "" || closed[event.Index] {
					return errors.New("delta outside an open content block")
				}
				switch event.Delta.Type {
				case "text_delta":
					if blocks[event.Index] != "text" {
						return errors.New("text delta in a different block type")
					}
					text.WriteString(event.Delta.Text)
					if err := emit(generationkit.GenerationEvent{Kind: "text.delta", Text: event.Delta.Text}); err != nil {
						return err
					}
				case "thinking_delta", "signature_delta":
					block := thinking[event.Index]
					if block == nil {
						return errors.New("thinking delta without block start")
					}
					block.Thinking += event.Delta.Thinking
					block.Signature += event.Delta.Signature
					if event.Delta.Thinking != "" {
						return emit(generationkit.GenerationEvent{Kind: "reasoning.delta", Index: event.Index, Text: event.Delta.Thinking})
					}
				case "input_json_delta":
					call := calls[event.Index]
					if call == nil {
						return errors.New("tool delta without tool start")
					}
					call.Arguments = append(call.Arguments, event.Delta.JSON...)
					if err := emit(generationkit.GenerationEvent{Kind: "tool.delta", Index: event.Index, CallID: call.ID, ArgumentsDelta: event.Delta.JSON}); err != nil {
						return err
					}
				default:
					return unsupported("Anthropic streamed delta " + event.Delta.Type)
				}
			case "message_delta":
				r.StopReason = normalizeStop(event.Delta.StopReason)
				r.Usage.Output = event.Usage.Output
				r.Usage.Total = r.Usage.Input + r.Usage.Output
			case "content_block_stop":
				if blocks[event.Index] == "" || closed[event.Index] {
					return errors.New("invalid content block stop")
				}
				closed[event.Index] = true
			case "message_stop":
				if !messageStarted {
					return errors.New("message stop without start")
				}
				for index := range blocks {
					if !closed[index] {
						return errors.New("message stop with incomplete block")
					}
				}
				terminal = r.StopReason != ""
			}
		case Gemini:
			chunk, err := c.decodeOffset(data, request.Model, len(calls))
			if err != nil {
				return err
			}
			if chunk.ID != "" {
				r.ID = chunk.ID
			}
			r.Model = chunk.Model
			if chunk.Usage.Total > 0 {
				r.Usage = chunk.Usage
			}
			for _, part := range chunk.Message.Parts {
				r.Message.Parts = append(r.Message.Parts, part)
				if part.Kind == "reasoning" {
					if err := emit(generationkit.GenerationEvent{Kind: "reasoning.delta", Text: part.Text}); err != nil {
						return err
					}
				}
				if part.Kind == "text" {
					if err := emit(generationkit.GenerationEvent{Kind: "text.delta", Text: part.Text}); err != nil {
						return err
					}
				}
			}
			for _, call := range chunk.Message.ToolCalls {
				index := len(calls)
				copy := call
				calls[index] = &copy
				if err := emit(generationkit.GenerationEvent{Kind: "tool.delta", Index: index, CallID: call.ID, ToolName: call.Name, ArgumentsDelta: string(call.Arguments)}); err != nil {
					return err
				}
			}
			if chunk.StopReason != "" {
				r.StopReason = chunk.StopReason
				terminal = true
			}
		}
		return nil
	})
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	if !terminal {
		return generationkit.GenerationResponse{}, io.ErrUnexpectedEOF
	}
	if reasoning.Len() > 0 {
		r.Message.Parts = append(r.Message.Parts, generationkit.MessagePart{Kind: "reasoning", Text: reasoning.String()})
	}
	if refusal.Len() > 0 {
		r.StopReason = "refusal"
		r.Message.Parts = append(r.Message.Parts, generationkit.MessagePart{Kind: "refusal", Text: refusal.String()})
	}
	thinkingIndexes := make([]int, 0, len(thinking))
	for index := range thinking {
		thinkingIndexes = append(thinkingIndexes, index)
	}
	sort.Ints(thinkingIndexes)
	for _, index := range thinkingIndexes {
		part, err := thinkingPart(*thinking[index])
		if err != nil {
			return generationkit.GenerationResponse{}, err
		}
		r.Message.Parts = append(r.Message.Parts, part)
	}
	if text.Len() > 0 {
		r.Message.Parts = append(r.Message.Parts, generationkit.MessagePart{Kind: "text", Text: text.String()})
	}
	indexes := make([]int, 0, len(calls))
	for index := range calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		call := calls[index]
		if len(call.Arguments) == 0 {
			call.Arguments = fallback[index]
		}
		r.Message.ToolCalls = append(r.Message.ToolCalls, *call)
	}
	if err := generationkit.ValidateGenerationResult(r, request); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	if err := emitCompleted(r, emit); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	return r, nil
}

// The callback owns its completion snapshot; mutations cannot change the
// validated response returned from Stream.
func emitCompleted(response generationkit.GenerationResponse, emit func(generationkit.GenerationEvent) error) error {
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	var snapshot generationkit.GenerationResponse
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	for index, call := range response.Message.ToolCalls {
		copied := call
		copied.Arguments = append(json.RawMessage(nil), call.Arguments...)
		copied.ProviderData = append(json.RawMessage(nil), call.ProviderData...)
		if err := emit(generationkit.GenerationEvent{Kind: "tool.completed", Index: index, CallID: call.ID, ToolName: call.Name, Call: &copied}); err != nil {
			return err
		}
	}
	usage := response.Usage
	if err := emit(generationkit.GenerationEvent{Kind: "usage", Usage: &usage}); err != nil {
		return err
	}
	return emit(generationkit.GenerationEvent{Kind: "completed", Response: &snapshot})
}

// readSSE handles comments, multi-line data, and final events without a trailing
// blank line. Both the complete stream and individual lines are bounded.
func readSSE(reader io.Reader, limit int64, consume func([]byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), int(limit)+1)
	var data []string
	var total int64
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		event := []byte(strings.Join(data, "\n"))
		data = nil
		return consume(event)
	}
	for scanner.Scan() {
		line := scanner.Text()
		total += int64(len(line) + 1)
		if total > limit {
			return errors.New("model stream exceeds configured byte limit")
		}
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}
