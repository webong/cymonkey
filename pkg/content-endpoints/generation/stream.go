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

	"blockade"
)

// Stream emits deltas without executing tool calls. Truncated streams fail
// rather than reporting a successful completion with partial tool arguments.
func (c *Client) Stream(ctx context.Context, request blockade.GenerationRequest, emit func(blockade.GenerationEvent) error) (blockade.GenerationResponse, error) {
	if emit == nil {
		return blockade.GenerationResponse{}, errors.New("stream callback is required")
	}
	if c.config.Protocol == Responses {
		return c.streamResponses(ctx, request, emit)
	}
	body, path, err := c.encode(request, true)
	if err != nil {
		return blockade.GenerationResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, path, "application/json", bytes.NewReader(body), true)
	if err != nil {
		return blockade.GenerationResponse{}, err
	}
	defer resp.Body.Close()
	r := blockade.GenerationResponse{Model: request.Model, Message: blockade.Message{Role: "assistant"}}
	calls := map[int]*blockade.ToolCall{}
	fallback := map[int]json.RawMessage{}
	terminal := false
	var text strings.Builder
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
				r.Usage = blockade.TokenUsage{Input: wire.Usage.Input, Output: wire.Usage.Output, Total: wire.Usage.Total}
			}
			for _, choice := range wire.Choices {
				if choice.Index != 0 {
					return unsupported("multiple streamed choices")
				}
				if choice.FinishReason != "" {
					r.StopReason = choice.FinishReason
				}
				delta := choice.Delta.Content
				if choice.Delta.Refusal != "" {
					delta = choice.Delta.Refusal
					r.StopReason = "refusal"
				}
				if delta != "" {
					text.WriteString(delta)
					if err := emit(blockade.GenerationEvent{Kind: "text.delta", Text: delta}); err != nil {
						return err
					}
				}
				for _, part := range choice.Delta.ToolCalls {
					if part.Index < 0 || part.Index > 1024 {
						return errors.New("invalid tool call index")
					}
					call := calls[part.Index]
					if call == nil {
						call = &blockade.ToolCall{}
						calls[part.Index] = call
					}
					if part.ID != "" {
						call.ID = part.ID
					}
					call.Name += part.Function.Name
					call.Arguments = append(call.Arguments, part.Function.Arguments...)
					if err := emit(blockade.GenerationEvent{Kind: "tool.delta", Index: part.Index, CallID: call.ID, ToolName: part.Function.Name, ArgumentsDelta: part.Function.Arguments}); err != nil {
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
				r.ID = event.Message.ID
				if event.Message.Model != "" {
					r.Model = event.Message.Model
				}
				r.Usage.Input = event.Message.Usage.Input
			case "content_block_start":
				if event.Index < 0 || event.Index > 1024 {
					return errors.New("invalid content block index")
				}
				switch event.Block.Type {
				case "tool_use":
					calls[event.Index] = &blockade.ToolCall{ID: event.Block.ID, Name: event.Block.Name}
					fallback[event.Index] = event.Block.Input
					if err := emit(blockade.GenerationEvent{Kind: "tool.delta", Index: event.Index, CallID: event.Block.ID, ToolName: event.Block.Name}); err != nil {
						return err
					}
				case "text":
					if event.Block.Text != "" {
						text.WriteString(event.Block.Text)
						if err := emit(blockade.GenerationEvent{Kind: "text.delta", Text: event.Block.Text}); err != nil {
							return err
						}
					}
				default:
					return unsupported("Anthropic streamed block " + event.Block.Type)
				}
			case "content_block_delta":
				switch event.Delta.Type {
				case "text_delta":
					text.WriteString(event.Delta.Text)
					if err := emit(blockade.GenerationEvent{Kind: "text.delta", Text: event.Delta.Text}); err != nil {
						return err
					}
				case "input_json_delta":
					call := calls[event.Index]
					if call == nil {
						return errors.New("tool delta without tool start")
					}
					call.Arguments = append(call.Arguments, event.Delta.JSON...)
					if err := emit(blockade.GenerationEvent{Kind: "tool.delta", Index: event.Index, CallID: call.ID, ArgumentsDelta: event.Delta.JSON}); err != nil {
						return err
					}
				default:
					return unsupported("Anthropic streamed delta " + event.Delta.Type)
				}
			case "message_delta":
				r.StopReason = normalizeStop(event.Delta.StopReason)
				r.Usage.Output = event.Usage.Output
				r.Usage.Total = r.Usage.Input + r.Usage.Output
			case "message_stop":
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
				if part.Kind == "text" {
					if err := emit(blockade.GenerationEvent{Kind: "text.delta", Text: part.Text}); err != nil {
						return err
					}
				}
			}
			for _, call := range chunk.Message.ToolCalls {
				index := len(calls)
				copy := call
				calls[index] = &copy
				if err := emit(blockade.GenerationEvent{Kind: "tool.delta", Index: index, CallID: call.ID, ToolName: call.Name, ArgumentsDelta: string(call.Arguments)}); err != nil {
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
		return blockade.GenerationResponse{}, err
	}
	if !terminal {
		return blockade.GenerationResponse{}, io.ErrUnexpectedEOF
	}
	if text.Len() > 0 {
		r.Message.Parts = append(r.Message.Parts, blockade.MessagePart{Kind: "text", Text: text.String()})
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
	if err := blockade.ValidateGenerationResponse(r); err != nil {
		return blockade.GenerationResponse{}, err
	}
	if err := emitCompleted(r, emit); err != nil {
		return blockade.GenerationResponse{}, err
	}
	return r, nil
}

// The callback owns its completion snapshot; mutations cannot change the
// validated response returned from Stream.
func emitCompleted(response blockade.GenerationResponse, emit func(blockade.GenerationEvent) error) error {
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	var snapshot blockade.GenerationResponse
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	return emit(blockade.GenerationEvent{Kind: "completed", Response: &snapshot})
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
