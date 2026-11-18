package generationkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"jangolova/contentkit"
	"math"
	"mime"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/webong/ext/pkg/plugin-go/contract/validate/jsonschema"
)

// Generation is independent of observation/interpretation. Providers produce
// messages and proposed tool calls; only the host may execute those calls.
type Message struct {
	Role       string        `json:"role"` // system, user, assistant, tool
	Parts      []MessagePart `json:"parts,omitempty"`
	ToolCalls  []ToolCall    `json:"toolCalls,omitempty"`
	ToolCallID string        `json:"toolCallId,omitempty"`
	ToolName   string        `json:"toolName,omitempty"`
	ToolError  bool          `json:"toolError,omitempty"`
}

type MessagePart struct {
	Kind      string          `json:"kind"` // text, reasoning, refusal, image, audio, video, file, context
	Text      string          `json:"text,omitempty"`
	MediaType string          `json:"mediaType,omitempty"`
	Data      []byte          `json:"data,omitempty"`
	Reference *MediaReference `json:"reference,omitempty"`
	// ProviderData preserves opaque provider context, such as thought signatures.
	// It is passed back only to that provider and is never treated as an action.
	ProviderData json.RawMessage `json:"providerData,omitempty"`
}

// MediaReference is passed to the selected adapter. Libraries never download
// URLs or upload local files implicitly. Provider file IDs are opaque.
type MediaReference struct {
	Kind string `json:"kind"` // url or file
	URI  string `json:"uri"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type ToolCall struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Arguments    json.RawMessage `json:"arguments"`
	ProviderData json.RawMessage `json:"providerData,omitempty"`
}

type GenerationRequest struct {
	Model           string           `json:"model"`
	Messages        []Message        `json:"messages"`
	Tools           []ToolDefinition `json:"tools,omitempty"`
	MaxOutputTokens int              `json:"maxOutputTokens,omitempty"`
	Temperature     *float64         `json:"temperature,omitempty"`
	OutputSchema    json.RawMessage  `json:"outputSchema,omitempty"`
	// Options are owned and validated by the explicitly selected adapter.
	Options json.RawMessage `json:"options,omitempty"`
}

type TokenUsage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	Total       int64 `json:"total"`
	CachedInput int64 `json:"cachedInput,omitempty"`
	Reasoning   int64 `json:"reasoning,omitempty"`
}

type GenerationResponse struct {
	ID         string     `json:"id,omitempty"`
	Model      string     `json:"model"`
	Message    Message    `json:"message"`
	StopReason string     `json:"stopReason,omitempty"`
	Usage      TokenUsage `json:"usage"`
}

// Text returns only text parts; tool calls and provider context remain separate.
func (r GenerationResponse) Text() string {
	var text strings.Builder
	for _, part := range r.Message.Parts {
		if part.Kind == "text" {
			text.WriteString(part.Text)
		}
	}
	return text.String()
}

type GenerationEvent struct {
	Kind           string              `json:"kind"` // started, text.delta, reasoning.delta, refusal.delta, tool.delta, tool.completed, usage, completed
	Text           string              `json:"text,omitempty"`
	Index          int                 `json:"index,omitempty"`
	CallID         string              `json:"callId,omitempty"`
	ToolName       string              `json:"toolName,omitempty"`
	ArgumentsDelta string              `json:"argumentsDelta,omitempty"`
	Response       *GenerationResponse `json:"response,omitempty"`
	Call           *ToolCall           `json:"call,omitempty"`
	Usage          *TokenUsage         `json:"usage,omitempty"`
}

type Generator interface {
	Generate(context.Context, GenerationRequest) (GenerationResponse, error)
}

// StreamingGenerator delivers events synchronously. An emit error stops the
// request, providing backpressure and cancellation without a background loop.
type StreamingGenerator interface {
	Stream(context.Context, GenerationRequest, func(GenerationEvent) error) (GenerationResponse, error)
}

func TextMessage(role, text string) Message {
	return Message{Role: role, Parts: []MessagePart{{Kind: "text", Text: text}}}
}

// ValidateGenerationRequest checks the neutral contract, not model-specific
// compatibility. Each provider rejects unsupported modalities/options explicitly.
func ValidateGenerationRequest(request GenerationRequest) error {
	if strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 || len(request.Messages) > 4096 || request.MaxOutputTokens < 0 {
		return fmt.Errorf("%w: model, messages, or token limit", contentkit.ErrInvalidGeneration)
	}
	if t := request.Temperature; t != nil && (math.IsNaN(*t) || math.IsInf(*t, 0) || *t < 0 || *t > 2) {
		return fmt.Errorf("%w: temperature outside [0,2]", contentkit.ErrInvalidGeneration)
	}
	seen := map[string]bool{}
	for _, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) == "" || seen[tool.Name] || !jsonObject(tool.InputSchema) {
			return fmt.Errorf("%w: tool definition", contentkit.ErrInvalidGeneration)
		}
		seen[tool.Name] = true
	}
	if len(request.OutputSchema) != 0 && !jsonObject(request.OutputSchema) {
		return fmt.Errorf("%w: output schema", contentkit.ErrInvalidGeneration)
	}
	if len(request.OutputSchema) != 0 {
		if _, err := jsonschema.Compile(request.OutputSchema); err != nil {
			return fmt.Errorf("%w: %w", contentkit.ErrInvalidGeneration, err)
		}
	}
	if len(request.Options) != 0 && !jsonObject(request.Options) {
		return fmt.Errorf("%w: adapter options must be an object", contentkit.ErrInvalidGeneration)
	}
	for _, message := range request.Messages {
		if err := validateMessage(message); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > 32<<20 {
		return fmt.Errorf("%w: request exceeds 32 MiB or cannot be encoded", contentkit.ErrInvalidGeneration)
	}
	return nil
}

func validateMessage(message Message) error {
	switch message.Role {
	case "system", "user", "assistant", "tool":
	default:
		return fmt.Errorf("%w: message role", contentkit.ErrInvalidGeneration)
	}
	if message.Role == "tool" && message.ToolCallID == "" {
		return fmt.Errorf("%w: tool result needs a call ID", contentkit.ErrInvalidGeneration)
	}
	if message.ToolError && message.Role != "tool" {
		return fmt.Errorf("%w: failure flag belongs to a tool result", contentkit.ErrInvalidGeneration)
	}
	if len(message.ToolCalls) > 0 && message.Role != "assistant" {
		return fmt.Errorf("%w: only assistant messages contain tool calls", contentkit.ErrInvalidGeneration)
	}
	if len(message.Parts) == 0 && len(message.ToolCalls) == 0 {
		return fmt.Errorf("%w: empty message", contentkit.ErrInvalidGeneration)
	}
	for _, part := range message.Parts {
		if len(part.ProviderData) > 0 && !json.Valid(part.ProviderData) {
			return fmt.Errorf("%w: provider context", contentkit.ErrInvalidGeneration)
		}
		switch part.Kind {
		case "context":
			if message.Role != "assistant" || len(part.ProviderData) == 0 || part.Text != "" || part.MediaType != "" || len(part.Data) != 0 || part.Reference != nil {
				return fmt.Errorf("%w: opaque model context", contentkit.ErrInvalidGeneration)
			}
		case "text", "reasoning", "refusal":
			if part.MediaType != "" || len(part.Data) != 0 || part.Reference != nil || part.Kind != "text" && message.Role != "assistant" {
				return fmt.Errorf("%w: mixed text/binary part", contentkit.ErrInvalidGeneration)
			}
		case "image", "audio", "video", "file":
			_, _, mediaErr := mime.ParseMediaType(part.MediaType)
			if part.Text != "" || (len(part.Data) == 0) == (part.Reference == nil) || mediaErr != nil || !strings.Contains(part.MediaType, "/") {
				return fmt.Errorf("%w: binary part", contentkit.ErrInvalidGeneration)
			}
			if ref := part.Reference; ref != nil {
				if ref.URI == "" || len(ref.URI) > 8192 {
					return fmt.Errorf("%w: media reference", contentkit.ErrInvalidGeneration)
				}
				switch ref.Kind {
				case "url":
					u, err := url.Parse(ref.URI)
					if err != nil || u.Host == "" || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
						return fmt.Errorf("%w: media URL must use HTTPS", contentkit.ErrInvalidGeneration)
					}
				case "file":
				default:
					return fmt.Errorf("%w: media reference kind", contentkit.ErrInvalidGeneration)
				}
			}
		default:
			return fmt.Errorf("%w: part kind", contentkit.ErrInvalidGeneration)
		}
	}
	ids := map[string]bool{}
	for _, call := range message.ToolCalls {
		if call.ID == "" || strings.TrimSpace(call.Name) == "" || ids[call.ID] || !jsonObject(call.Arguments) || len(call.ProviderData) > 0 && !json.Valid(call.ProviderData) {
			return fmt.Errorf("%w: tool call", contentkit.ErrInvalidGeneration)
		}
		ids[call.ID] = true
	}
	return nil
}

func ValidateGenerationResponse(response GenerationResponse) error {
	if response.Model == "" || response.Message.Role != "assistant" || response.Usage.Input < 0 || response.Usage.Output < 0 || response.Usage.Total < 0 || response.Usage.CachedInput < 0 || response.Usage.Reasoning < 0 {
		return fmt.Errorf("%w: response metadata", contentkit.ErrInvalidGeneration)
	}
	return validateMessage(response.Message)
}

// ValidateGenerationResult includes requested output constraints. Truncated
// output, refusals and proposals cannot be decoded as successful objects.
func ValidateGenerationResult(response GenerationResponse, request GenerationRequest) error {
	if err := ValidateGenerationResponse(response); err != nil {
		return err
	}
	refused := response.StopReason == "refusal"
	for _, part := range response.Message.Parts {
		if part.Kind == "refusal" {
			refused = true
		}
	}
	if len(request.OutputSchema) > 0 && len(response.Message.ToolCalls) == 0 && !refused {
		return ValidateStructuredOutput(response, request.OutputSchema)
	}
	return nil
}

func jsonObject(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	return json.Unmarshal(raw, &value) == nil && value != nil
}

// ModelInfo is host-registered metadata. Capabilities describe the selected
// model, not a guess based on its name or on the endpoint protocol.
type ModelInfo struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model,omitempty"` // provider's model ID, if different from ID
	Capabilities []string `json:"capabilities"`
}

// ModelCatalog contains explicitly registered model bindings. It has no
// global defaults, credential store, fallback routing, or automatic retries.
type ModelCatalog struct {
	mu     sync.RWMutex
	models map[string]modelBinding
}
type modelBinding struct {
	info      ModelInfo
	generator Generator
}

func (c *ModelCatalog) Register(info ModelInfo, generator Generator) error {
	if info.ID == "" || info.Provider == "" || generator == nil {
		return errors.New("model ID, provider, and generator are required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.models == nil {
		c.models = map[string]modelBinding{}
	}
	if _, exists := c.models[info.ID]; exists {
		return fmt.Errorf("model %q is already registered", info.ID)
	}
	info.Capabilities = append([]string(nil), info.Capabilities...)
	c.models[info.ID] = modelBinding{info, generator}
	return nil
}

func (c *ModelCatalog) Models() []ModelInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	models := make([]ModelInfo, 0, len(c.models))
	for _, binding := range c.models {
		info := binding.info
		info.Capabilities = append([]string(nil), info.Capabilities...)
		models = append(models, info)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

func (c *ModelCatalog) Lookup(id string) (Generator, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	binding, ok := c.models[id]
	return binding.generator, ok
}

func (c *ModelCatalog) binding(id string) (modelBinding, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	binding, ok := c.models[id]
	if !ok {
		return modelBinding{}, fmt.Errorf("model binding %q is not registered", id)
	}
	return binding, nil
}
func (c *ModelCatalog) Generate(ctx context.Context, request GenerationRequest) (GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return GenerationResponse{}, err
	}
	if err := ValidateGenerationRequest(request); err != nil {
		return GenerationResponse{}, err
	}
	binding, err := c.binding(request.Model)
	if err != nil {
		return GenerationResponse{}, err
	}
	if binding.info.Model != "" {
		request.Model = binding.info.Model
	}
	response, err := binding.generator.Generate(ctx, request)
	if err != nil {
		return GenerationResponse{}, err
	}
	if err := ValidateGenerationResult(response, request); err != nil {
		return GenerationResponse{}, err
	}
	return response, nil
}
func (c *ModelCatalog) Stream(ctx context.Context, request GenerationRequest, emit func(GenerationEvent) error) (GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return GenerationResponse{}, err
	}
	if err := ValidateGenerationRequest(request); err != nil {
		return GenerationResponse{}, err
	}
	if emit == nil {
		return GenerationResponse{}, errors.New("stream callback is required")
	}
	binding, err := c.binding(request.Model)
	if err != nil {
		return GenerationResponse{}, err
	}
	provider, ok := binding.generator.(StreamingGenerator)
	if !ok {
		return GenerationResponse{}, contentkit.ErrGenerationUnsupported
	}
	if binding.info.Model != "" {
		request.Model = binding.info.Model
	}
	response, err := provider.Stream(ctx, request, emit)
	if err != nil {
		return GenerationResponse{}, err
	}
	if err := ValidateGenerationResult(response, request); err != nil {
		return GenerationResponse{}, err
	}
	return response, nil
}
