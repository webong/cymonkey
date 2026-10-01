package generation

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"blockade"
)

func (c *Client) encode(request blockade.GenerationRequest, stream bool) ([]byte, string, error) {
	if err := blockade.ValidateGenerationRequest(request); err != nil {
		return nil, "", err
	}
	var payload map[string]any
	var path string
	var err error
	switch c.config.Protocol {
	case Responses:
		payload, err = responsesRequest(request)
		path = "/responses"
		if err == nil && stream {
			payload["stream"] = true
		}
	case OpenAI:
		payload, err = openAIRequest(request)
		path = "/chat/completions"
		if err == nil && stream {
			payload["stream"] = true
			payload["stream_options"] = map[string]any{"include_usage": true}
		}
	case Anthropic:
		payload, err = anthropicRequest(request)
		path = "/messages"
		if err == nil && stream {
			payload["stream"] = true
		}
	case Gemini:
		payload, err = geminiRequest(request)
		path = "/models/" + url.PathEscape(request.Model) + ":generateContent"
		if stream {
			path = "/models/" + url.PathEscape(request.Model) + ":streamGenerateContent?alt=sse"
		}
	}
	if err != nil {
		return nil, "", err
	}
	data, err := json.Marshal(payload)
	return data, path, err
}

func unsupported(feature string) error {
	return fmt.Errorf("%w: %s", blockade.ErrGenerationUnsupported, feature)
}
func plainText(message blockade.Message) (string, error) {
	var text strings.Builder
	for _, part := range message.Parts {
		if part.Kind != "text" {
			return "", unsupported("non-text content in this message role")
		}
		text.WriteString(part.Text)
	}
	return text.String(), nil
}
func openAIRequest(request blockade.GenerationRequest) (map[string]any, error) {
	messages := make([]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		wire := map[string]any{"role": message.Role}
		if message.Role == "tool" || message.Role == "system" || message.Role == "assistant" {
			text, err := plainText(message)
			if err != nil {
				return nil, err
			}
			wire["content"] = text
		} else {
			parts := make([]any, 0, len(message.Parts))
			for _, part := range message.Parts {
				switch part.Kind {
				case "text":
					parts = append(parts, map[string]any{"type": "text", "text": part.Text})
				case "image":
					parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + part.MediaType + ";base64," + base64.StdEncoding.EncodeToString(part.Data)}})
				case "audio":
					format := ""
					switch part.MediaType {
					case "audio/wav":
						format = "wav"
					case "audio/mpeg":
						format = "mp3"
					default:
						return nil, unsupported("OpenAI audio format")
					}
					parts = append(parts, map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": base64.StdEncoding.EncodeToString(part.Data), "format": format}})
				default:
					return nil, unsupported("OpenAI-compatible " + part.Kind + " input")
				}
			}
			wire["content"] = parts
		}
		if message.Role == "tool" {
			wire["tool_call_id"] = message.ToolCallID
		}
		if len(message.ToolCalls) > 0 {
			calls := make([]any, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				calls = append(calls, map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)}})
			}
			wire["tool_calls"] = calls
		}
		messages = append(messages, wire)
	}
	payload := map[string]any{"model": request.Model, "messages": messages}
	if request.MaxOutputTokens > 0 {
		payload["max_completion_tokens"] = request.MaxOutputTokens
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.InputSchema}})
		}
		payload["tools"] = tools
	}
	if len(request.OutputSchema) > 0 {
		payload["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "output", "strict": true, "schema": request.OutputSchema}}
	}
	return payload, nil
}

func anthropicRequest(request blockade.GenerationRequest) (map[string]any, error) {
	messages := []any{}
	system := []any{}
	for _, message := range request.Messages {
		if message.Role == "system" {
			text, err := plainText(message)
			if err != nil {
				return nil, err
			}
			system = append(system, map[string]any{"type": "text", "text": text})
			continue
		}
		blocks := []any{}
		role := message.Role
		if role == "tool" {
			text, err := plainText(message)
			if err != nil {
				return nil, err
			}
			role = "user"
			blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": message.ToolCallID, "content": text})
		} else {
			for _, part := range message.Parts {
				switch part.Kind {
				case "text":
					blocks = append(blocks, map[string]any{"type": "text", "text": part.Text})
				case "image":
					blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": part.MediaType, "data": base64.StdEncoding.EncodeToString(part.Data)}})
				case "file":
					if part.MediaType != "application/pdf" {
						return nil, unsupported("Anthropic file format")
					}
					blocks = append(blocks, map[string]any{"type": "document", "source": map[string]any{"type": "base64", "media_type": part.MediaType, "data": base64.StdEncoding.EncodeToString(part.Data)}})
				default:
					return nil, unsupported("Anthropic " + part.Kind + " input")
				}
			}
		}
		for _, call := range message.ToolCalls {
			blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": call.Arguments})
		}
		messages = append(messages, map[string]any{"role": role, "content": blocks})
	}
	maxTokens := request.MaxOutputTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	payload := map[string]any{"model": request.Model, "messages": messages, "max_tokens": maxTokens}
	if len(request.OutputSchema) > 0 {
		payload["output_config"] = map[string]any{"format": map[string]any{"type": "json_schema", "schema": request.OutputSchema}}
	}
	if len(system) > 0 {
		payload["system"] = system
	}
	if request.Temperature != nil {
		if *request.Temperature > 1 {
			return nil, unsupported("Anthropic temperature above 1")
		}
		payload["temperature"] = *request.Temperature
	}
	if len(request.Tools) > 0 {
		tools := []any{}
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.InputSchema})
		}
		payload["tools"] = tools
	}
	return payload, nil
}

func geminiRequest(request blockade.GenerationRequest) (map[string]any, error) {
	contents := []any{}
	system := []any{}
	for _, message := range request.Messages {
		if message.Role == "system" {
			text, err := plainText(message)
			if err != nil {
				return nil, err
			}
			system = append(system, map[string]any{"text": text})
			continue
		}
		parts := []any{}
		role := "user"
		if message.Role == "assistant" {
			role = "model"
		}
		if message.Role == "tool" {
			if message.ToolName == "" {
				return nil, unsupported("Gemini tool result without tool name")
			}
			text, err := plainText(message)
			if err != nil {
				return nil, err
			}
			response := map[string]any{"result": text}
			var object map[string]any
			if json.Unmarshal([]byte(text), &object) == nil && object != nil {
				response = object
			}
			parts = append(parts, map[string]any{"functionResponse": map[string]any{"id": message.ToolCallID, "name": message.ToolName, "response": response}})
		} else {
			for _, part := range message.Parts {
				wire := map[string]any{}
				if part.Kind == "text" {
					wire["text"] = part.Text
				} else if part.Kind != "context" {
					wire["inlineData"] = map[string]any{"mimeType": part.MediaType, "data": base64.StdEncoding.EncodeToString(part.Data)}
				}
				if len(part.ProviderData) > 0 {
					var context map[string]any
					if json.Unmarshal(part.ProviderData, &context) != nil {
						return nil, unsupported("Gemini provider context")
					}
					if signature, ok := context["thoughtSignature"]; ok {
						wire["thoughtSignature"] = signature
					}
				}
				if part.Kind == "context" && wire["thoughtSignature"] == nil {
					return nil, unsupported("foreign model context")
				}
				parts = append(parts, wire)
			}
		}
		for _, call := range message.ToolCalls {
			wire := map[string]any{"functionCall": map[string]any{"id": call.ID, "name": call.Name, "args": call.Arguments}}
			if len(call.ProviderData) > 0 {
				var context map[string]any
				if json.Unmarshal(call.ProviderData, &context) != nil {
					return nil, unsupported("Gemini provider context")
				}
				if signature, ok := context["thoughtSignature"]; ok {
					wire["thoughtSignature"] = signature
				}
			}
			parts = append(parts, wire)
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	payload := map[string]any{"contents": contents}
	if len(system) > 0 {
		payload["systemInstruction"] = map[string]any{"parts": system}
	}
	config := map[string]any{}
	if request.MaxOutputTokens > 0 {
		config["maxOutputTokens"] = request.MaxOutputTokens
	}
	if request.Temperature != nil {
		config["temperature"] = *request.Temperature
	}
	if len(request.OutputSchema) > 0 {
		config["responseMimeType"] = "application/json"
		config["responseJsonSchema"] = request.OutputSchema
	}
	if len(config) > 0 {
		payload["generationConfig"] = config
	}
	if len(request.Tools) > 0 {
		declarations := []any{}
		for _, tool := range request.Tools {
			declarations = append(declarations, map[string]any{"name": tool.Name, "description": tool.Description, "parametersJsonSchema": tool.InputSchema})
		}
		payload["tools"] = []any{map[string]any{"functionDeclarations": declarations}}
	}
	return payload, nil
}

type wireToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type wireMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Refusal   string         `json:"refusal"`
	ToolCalls []wireToolCall `json:"tool_calls"`
}
type openAIResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int         `json:"index"`
		Message      wireMessage `json:"message"`
		Delta        wireMessage `json:"delta"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		Input  int64 `json:"prompt_tokens"`
		Output int64 `json:"completion_tokens"`
		Total  int64 `json:"total_tokens"`
	} `json:"usage"`
	Error json.RawMessage `json:"error"`
}
type anthropicBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
type anthropicResponse struct {
	ID         string           `json:"id"`
	Model      string           `json:"model"`
	Content    []anthropicBlock `json:"content"`
	StopReason string           `json:"stop_reason"`
	Usage      struct {
		Input  int64 `json:"input_tokens"`
		Output int64 `json:"output_tokens"`
	} `json:"usage"`
}
type geminiPart struct {
	Text                string          `json:"text"`
	Thought             bool            `json:"thought"`
	ThoughtSignature    string          `json:"thoughtSignature"`
	InlineData          json.RawMessage `json:"inlineData"`
	FileData            json.RawMessage `json:"fileData"`
	ExecutableCode      json.RawMessage `json:"executableCode"`
	CodeExecutionResult json.RawMessage `json:"codeExecutionResult"`
	FunctionCall        *struct {
		ID   string          `json:"id"`
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"functionCall"`
}
type geminiResponse struct {
	ResponseID string `json:"responseId"`
	Model      string `json:"modelVersion"`
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Usage struct {
		Input  int64 `json:"promptTokenCount"`
		Output int64 `json:"candidatesTokenCount"`
		Total  int64 `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	Error json.RawMessage `json:"error"`
}

func (c *Client) decode(data []byte, model string) (blockade.GenerationResponse, error) {
	return c.decodeOffset(data, model, 0)
}

func (c *Client) decodeOffset(data []byte, model string, callOffset int) (blockade.GenerationResponse, error) {
	r := blockade.GenerationResponse{Model: model, Message: blockade.Message{Role: "assistant"}}
	switch c.config.Protocol {
	case Responses:
		return decodeResponses(data, model)
	case OpenAI:
		var wire openAIResponse
		if err := json.Unmarshal(data, &wire); err != nil {
			return r, err
		}
		if hasError(wire.Error) || len(wire.Choices) != 1 {
			return r, fmt.Errorf("invalid OpenAI response")
		}
		r.ID = wire.ID
		if wire.Model != "" {
			r.Model = wire.Model
		}
		m := wire.Choices[0].Message
		if m.Content != "" {
			r.Message.Parts = append(r.Message.Parts, blockade.MessagePart{Kind: "text", Text: m.Content})
		}
		if m.Refusal != "" {
			r.Message.Parts = append(r.Message.Parts, blockade.MessagePart{Kind: "text", Text: m.Refusal})
			r.StopReason = "refusal"
		}
		for _, call := range m.ToolCalls {
			if call.Type != "function" {
				return r, unsupported("non-function tool call")
			}
			r.Message.ToolCalls = append(r.Message.ToolCalls, blockade.ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)})
		}
		if r.StopReason == "" {
			r.StopReason = wire.Choices[0].FinishReason
		}
		r.Usage = blockade.TokenUsage{Input: wire.Usage.Input, Output: wire.Usage.Output, Total: wire.Usage.Total}
	case Anthropic:
		var wire anthropicResponse
		if err := json.Unmarshal(data, &wire); err != nil {
			return r, err
		}
		r.ID = wire.ID
		if wire.Model != "" {
			r.Model = wire.Model
		}
		for _, block := range wire.Content {
			switch block.Type {
			case "text":
				r.Message.Parts = append(r.Message.Parts, blockade.MessagePart{Kind: "text", Text: block.Text})
			case "tool_use":
				r.Message.ToolCalls = append(r.Message.ToolCalls, blockade.ToolCall{ID: block.ID, Name: block.Name, Arguments: block.Input})
			default:
				return r, unsupported("Anthropic response block " + block.Type)
			}
		}
		r.StopReason = normalizeStop(wire.StopReason)
		r.Usage = blockade.TokenUsage{Input: wire.Usage.Input, Output: wire.Usage.Output, Total: wire.Usage.Input + wire.Usage.Output}
	case Gemini:
		var wire geminiResponse
		if err := json.Unmarshal(data, &wire); err != nil {
			return r, err
		}
		if hasError(wire.Error) || len(wire.Candidates) > 1 || len(wire.Candidates) == 0 && wire.Usage.Total == 0 {
			return r, fmt.Errorf("invalid Gemini response or blocked prompt")
		}
		r.ID = wire.ResponseID
		if wire.Model != "" {
			r.Model = wire.Model
		}
		r.Usage = blockade.TokenUsage{Input: wire.Usage.Input, Output: wire.Usage.Output, Total: wire.Usage.Total}
		if len(wire.Candidates) == 0 {
			return r, nil
		}
		for _, part := range wire.Candidates[0].Content.Parts {
			if part.Thought || hasError(part.InlineData) || hasError(part.FileData) || hasError(part.ExecutableCode) || hasError(part.CodeExecutionResult) {
				return r, unsupported("Gemini non-text output part")
			}
			var signature json.RawMessage
			if part.ThoughtSignature != "" {
				signature, _ = json.Marshal(map[string]string{"thoughtSignature": part.ThoughtSignature})
			}
			if part.FunctionCall != nil {
				call := part.FunctionCall
				id := call.ID
				if id == "" {
					id = fmt.Sprintf("gemini-%s-%d", r.ID, callOffset+len(r.Message.ToolCalls))
				}
				args := call.Args
				if len(args) == 0 {
					args = json.RawMessage(`{}`)
				}
				r.Message.ToolCalls = append(r.Message.ToolCalls, blockade.ToolCall{ID: id, Name: call.Name, Arguments: args, ProviderData: signature})
			} else if part.Text != "" {
				r.Message.Parts = append(r.Message.Parts, blockade.MessagePart{Kind: "text", Text: part.Text, ProviderData: signature})
			} else if len(signature) > 0 {
				r.Message.Parts = append(r.Message.Parts, blockade.MessagePart{Kind: "context", ProviderData: signature})
			}
		}
		r.StopReason = normalizeStop(wire.Candidates[0].FinishReason)
		r.Usage = blockade.TokenUsage{Input: wire.Usage.Input, Output: wire.Usage.Output, Total: wire.Usage.Total}
	}
	return r, nil
}

func hasError(raw json.RawMessage) bool {
	return len(raw) > 0 && strings.TrimSpace(string(raw)) != "null"
}
func normalizeStop(reason string) string {
	switch reason {
	case "end_turn", "STOP":
		return "stop"
	case "max_tokens", "MAX_TOKENS":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}
