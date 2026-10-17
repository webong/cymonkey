package generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"kit/generationkit"
)

func responsesRequest(request generationkit.GenerationRequest) (map[string]any, error) {
	input := []any{}
	for _, message := range request.Messages {
		if message.Role == "tool" {
			text, err := plainText(message)
			if err != nil {
				return nil, err
			}
			input = append(input, map[string]any{"type": "function_call_output", "call_id": message.ToolCallID, "output": text})
			continue
		}
		parts := []any{}
		for _, part := range message.Parts {
			switch part.Kind {
			case "context":
				var item map[string]any
				if json.Unmarshal(part.ProviderData, &item) != nil || item["type"] != "reasoning" {
					return nil, unsupported("Responses context item")
				}
				input = append(input, part.ProviderData)
			case "text":
				kind := "input_text"
				if message.Role == "assistant" {
					kind = "output_text"
				}
				wire := map[string]any{"type": kind, "text": part.Text}
				if message.Role == "assistant" {
					wire["annotations"] = []any{}
				}
				parts = append(parts, wire)
			case "image":
				wire := map[string]any{"type": "input_image"}
				if part.Reference == nil {
					wire["image_url"] = "data:" + part.MediaType + ";base64," + base64.StdEncoding.EncodeToString(part.Data)
				} else if part.Reference.Kind == "url" {
					wire["image_url"] = part.Reference.URI
				} else {
					wire["file_id"] = part.Reference.URI
				}
				parts = append(parts, wire)
			case "file":
				filename := "input"
				if extensions, err := mime.ExtensionsByType(part.MediaType); err == nil && len(extensions) > 0 {
					filename += extensions[0]
				}
				wire := map[string]any{"type": "input_file"}
				if part.Reference == nil {
					wire["filename"] = filename
					wire["file_data"] = "data:" + part.MediaType + ";base64," + base64.StdEncoding.EncodeToString(part.Data)
				} else if part.Reference.Kind == "url" {
					wire["file_url"] = part.Reference.URI
				} else {
					wire["file_id"] = part.Reference.URI
				}
				parts = append(parts, wire)
			default:
				return nil, unsupported("Responses " + part.Kind + " input")
			}
		}
		if len(parts) > 0 {
			input = append(input, map[string]any{"type": "message", "role": message.Role, "content": parts})
		}
		for _, call := range message.ToolCalls {
			input = append(input, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": string(call.Arguments)})
		}
	}
	payload := map[string]any{"model": request.Model, "input": input, "store": false, "include": []string{"reasoning.encrypted_content"}}
	if request.MaxOutputTokens > 0 {
		payload["max_output_tokens"] = request.MaxOutputTokens
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if len(request.Tools) > 0 {
		tools := []any{}
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.InputSchema})
		}
		payload["tools"] = tools
	}
	if len(request.OutputSchema) > 0 {
		payload["text"] = map[string]any{"format": map[string]any{"type": "json_schema", "name": "output", "strict": true, "schema": request.OutputSchema}}
	}
	return payload, nil
}

func decodeResponses(data []byte, model string) (generationkit.GenerationResponse, error) {
	var wire struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		Status     string `json:"status"`
		Incomplete struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []json.RawMessage `json:"output"`
		Usage  struct {
			Input        int64 `json:"input_tokens"`
			Output       int64 `json:"output_tokens"`
			Total        int64 `json:"total_tokens"`
			InputDetails struct {
				Cached int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputDetails struct {
				Reasoning int64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	if wire.Status != "completed" && wire.Status != "incomplete" {
		return generationkit.GenerationResponse{}, errors.New("Responses request did not complete")
	}
	r := generationkit.GenerationResponse{ID: wire.ID, Model: model, Message: generationkit.Message{Role: "assistant"}, StopReason: "stop", Usage: generationkit.TokenUsage{Input: wire.Usage.Input, Output: wire.Usage.Output, Total: wire.Usage.Total, CachedInput: wire.Usage.InputDetails.Cached, Reasoning: wire.Usage.OutputDetails.Reasoning}}
	if wire.Model != "" {
		r.Model = wire.Model
	}
	if wire.Status == "incomplete" {
		r.StopReason = "incomplete"
		if wire.Incomplete.Reason == "max_output_tokens" {
			r.StopReason = "length"
		}
		if r.StopReason == "" {
			r.StopReason = "incomplete"
		}
	}
	for _, raw := range wire.Output {
		var item struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return r, err
		}
		switch item.Type {
		case "reasoning":
			r.Message.Parts = append(r.Message.Parts, generationkit.MessagePart{Kind: "context", ProviderData: append(json.RawMessage(nil), raw...)})
		case "message":
			for _, part := range item.Content {
				switch part.Type {
				case "output_text":
					r.Message.Parts = append(r.Message.Parts, generationkit.MessagePart{Kind: "text", Text: part.Text})
				case "refusal":
					r.Message.Parts = append(r.Message.Parts, generationkit.MessagePart{Kind: "refusal", Text: part.Refusal})
					r.StopReason = "refusal"
				default:
					return r, unsupported("Responses content " + part.Type)
				}
			}
		case "function_call":
			r.Message.ToolCalls = append(r.Message.ToolCalls, generationkit.ToolCall{ID: item.CallID, Name: item.Name, Arguments: json.RawMessage(item.Arguments)})
			if wire.Status == "completed" {
				r.StopReason = "tool_calls"
			}
		default:
			return r, unsupported("Responses output item " + item.Type)
		}
	}
	return r, nil
}

func (c *Client) streamResponses(ctx context.Context, request generationkit.GenerationRequest, emit func(generationkit.GenerationEvent) error) (generationkit.GenerationResponse, error) {
	body, path, err := c.encode(request, true)
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, path, "application/json", bytes.NewReader(body), true)
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	defer resp.Body.Close()
	var result generationkit.GenerationResponse
	complete := false
	err = readSSE(io.LimitReader(resp.Body, c.config.MaxResponseBytes+1), c.config.MaxResponseBytes, func(data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if complete {
			return errors.New("Responses event follows terminal response")
		}
		var event struct {
			Type  string `json:"type"`
			Index int    `json:"output_index"`
			Delta string `json:"delta"`
			Item  struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
				Name   string `json:"name"`
			} `json:"item"`
			Response json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		switch event.Type {
		case "response.reasoning_summary_text.delta":
			return emit(generationkit.GenerationEvent{Kind: "reasoning.delta", Text: event.Delta})
		case "response.refusal.delta":
			return emit(generationkit.GenerationEvent{Kind: "refusal.delta", Text: event.Delta})
		case "response.output_text.delta":
			return emit(generationkit.GenerationEvent{Kind: "text.delta", Text: event.Delta})
		case "response.output_item.added":
			if event.Item.Type == "function_call" {
				return emit(generationkit.GenerationEvent{Kind: "tool.delta", Index: event.Index, CallID: event.Item.CallID, ToolName: event.Item.Name})
			}
		case "response.function_call_arguments.delta":
			return emit(generationkit.GenerationEvent{Kind: "tool.delta", Index: event.Index, ArgumentsDelta: event.Delta})
		case "response.completed", "response.incomplete":
			var err error
			result, err = decodeResponses(event.Response, request.Model)
			if err != nil {
				return err
			}
			complete = true
		case "response.failed", "error":
			return errors.New("Responses stream failed")
		}
		return nil
	})
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	if !complete {
		return generationkit.GenerationResponse{}, io.ErrUnexpectedEOF
	}
	if err := generationkit.ValidateGenerationResult(result, request); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	if err := emitCompleted(result, emit); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	return result, nil
}
