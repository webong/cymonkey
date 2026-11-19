package console

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"jangolova/contentkit"

	"github.com/webong/ext/pkg/ext-go/engine/validate/jsonschema"
	"github.com/webong/ext/res/int/inference"
)

// The model contract is ext's res/int/inference. This file holds the
// Console's composition on top of it: a text message helper, the result check
// every model call goes through, and a router over explicitly registered
// models.

// TextMessage is a message with a single text part.
func TextMessage(role inference.Role, text string) inference.Message {
	return inference.Message{Role: role, Parts: []inference.Part{{Kind: inference.PartKindText, Text: text}}}
}

// ValidateSchema checks a value against a JSON Schema with ext's offline
// validator. It is the SchemaValidator for structured output.
func ValidateSchema(schema, value json.RawMessage) error { return jsonschema.Validate(schema, value) }

// ValidateResult checks a response against the request that produced it. A
// request with an output schema must come back as completed structured output
// unless the model proposed tool calls or refused.
func ValidateResult(response inference.GenerationResponse, request inference.GenerationRequest) error {
	if err := response.Validate(); err != nil {
		return err
	}
	if len(request.OutputSchema) > 0 && !response.HasToolCalls() && !response.IsRefusal() && response.StopReason != inference.FinishReasonRefusal {
		return inference.ValidateStructuredOutput(response, request.OutputSchema, ValidateSchema)
	}
	return nil
}

// ModelBinding registers one model. Info.ID is the name callers use; Model,
// when set, is the provider's own model ID sent in requests.
type ModelBinding struct {
	Info      inference.ModelInfo
	Model     string
	Generator inference.Generator
}

// Models routes requests to explicitly registered models. It has no global
// defaults, credential store, fallback routing, or automatic retries.
type Models struct {
	mu       sync.RWMutex
	bindings map[string]ModelBinding
}

func (m *Models) Register(binding ModelBinding) error {
	if binding.Info.ID == "" || binding.Info.Provider == "" || binding.Generator == nil {
		return errors.New("model ID, provider, and generator are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bindings == nil {
		m.bindings = map[string]ModelBinding{}
	}
	if _, exists := m.bindings[binding.Info.ID]; exists {
		return fmt.Errorf("model %q is already registered", binding.Info.ID)
	}
	binding.Info.Capabilities = append([]inference.Capability(nil), binding.Info.Capabilities...)
	m.bindings[binding.Info.ID] = binding
	return nil
}

// Models lists registered model metadata, ordered by ID.
func (m *Models) Models() []inference.ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	models := make([]inference.ModelInfo, 0, len(m.bindings))
	for _, binding := range m.bindings {
		info := binding.Info
		info.Capabilities = append([]inference.Capability(nil), info.Capabilities...)
		models = append(models, info)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

// Lookup returns the generator registered under id.
func (m *Models) Lookup(id string) (inference.Generator, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	binding, ok := m.bindings[id]
	return binding.Generator, ok
}

func (m *Models) binding(id string) (ModelBinding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	binding, ok := m.bindings[id]
	if !ok {
		return ModelBinding{}, fmt.Errorf("model binding %q is not registered", id)
	}
	return binding, nil
}

func (m *Models) Generate(ctx context.Context, request inference.GenerationRequest) (inference.GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return inference.GenerationResponse{}, err
	}
	if err := request.Validate(); err != nil {
		return inference.GenerationResponse{}, err
	}
	binding, err := m.binding(request.Model)
	if err != nil {
		return inference.GenerationResponse{}, err
	}
	if binding.Model != "" {
		request.Model = binding.Model
	}
	response, err := binding.Generator.Generate(ctx, request)
	if err != nil {
		return inference.GenerationResponse{}, err
	}
	if err := ValidateResult(response, request); err != nil {
		return inference.GenerationResponse{}, err
	}
	return response, nil
}

func (m *Models) Stream(ctx context.Context, request inference.GenerationRequest, sink inference.EventSink) (inference.GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return inference.GenerationResponse{}, err
	}
	if err := request.Validate(); err != nil {
		return inference.GenerationResponse{}, err
	}
	if sink == nil {
		return inference.GenerationResponse{}, errors.New("stream callback is required")
	}
	binding, err := m.binding(request.Model)
	if err != nil {
		return inference.GenerationResponse{}, err
	}
	streamer, ok := binding.Generator.(inference.Streamer)
	if !ok {
		return inference.GenerationResponse{}, contentkit.ErrGenerationUnsupported
	}
	if binding.Model != "" {
		request.Model = binding.Model
	}
	response, err := streamer.Stream(ctx, request, sink)
	if err != nil {
		return inference.GenerationResponse{}, err
	}
	if err := ValidateResult(response, request); err != nil {
		return inference.GenerationResponse{}, err
	}
	return response, nil
}

// EmbeddingOptions are the provider options the Console's embedding clients
// understand, carried in inference.EmbeddingRequest.Options.
type EmbeddingOptions struct {
	// Dimensions requests a shorter vector where the provider supports it.
	Dimensions int `json:"dimensions,omitempty"`
}

// DecodeEmbeddingOptions reads EmbeddingOptions strictly: unknown keys fail.
func DecodeEmbeddingOptions(raw json.RawMessage) (EmbeddingOptions, error) {
	var options EmbeddingOptions
	if err := decodeOptions(raw, &options); err != nil || options.Dimensions < 0 || options.Dimensions > 65536 {
		return EmbeddingOptions{}, fmt.Errorf("%w: embedding options", inference.ErrInvalidRequest)
	}
	return options, nil
}

// TranscriptionOptions are carried in inference.TranscriptionRequest.Options.
type TranscriptionOptions struct {
	// Metadata asks for language, duration and segments where supported.
	Metadata bool `json:"metadata,omitempty"`
}

func DecodeTranscriptionOptions(raw json.RawMessage) (TranscriptionOptions, error) {
	var options TranscriptionOptions
	if err := decodeOptions(raw, &options); err != nil {
		return TranscriptionOptions{}, fmt.Errorf("%w: transcription options", inference.ErrInvalidRequest)
	}
	return options, nil
}

// SpeechOptions are carried in inference.SpeechRequest.Options.
type SpeechOptions struct {
	// Instructions steer delivery where the provider supports it.
	Instructions string `json:"instructions,omitempty"`
}

func DecodeSpeechOptions(raw json.RawMessage) (SpeechOptions, error) {
	var options SpeechOptions
	if err := decodeOptions(raw, &options); err != nil || len(options.Instructions) > 4096 {
		return SpeechOptions{}, fmt.Errorf("%w: speech options", inference.ErrInvalidRequest)
	}
	return options, nil
}

// SpeechMediaType is the media type of a speech format. An empty format is mp3.
func SpeechMediaType(format string) (string, bool) {
	if format == "" {
		format = "mp3"
	}
	mediaType, ok := map[string]string{"mp3": "audio/mpeg", "opus": "audio/ogg", "aac": "audio/aac", "flac": "audio/flac", "wav": "audio/wav", "pcm": "audio/pcm"}[format]
	return mediaType, ok
}

func decodeOptions(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

// ValidateEmbeddings checks a response against its request: one vector per
// input, at the requested or a consistent dimension, with finite values.
func ValidateEmbeddings(response inference.EmbeddingResponse, request inference.EmbeddingRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if err := response.Validate(); err != nil {
		return err
	}
	options, err := DecodeEmbeddingOptions(request.Options)
	if err != nil {
		return err
	}
	if len(response.Embeddings) != len(request.Inputs) || response.Usage.Output < 0 {
		return fmt.Errorf("%w: embedding response metadata", contentkit.ErrInvalidGeneration)
	}
	dimension := options.Dimensions
	if dimension == 0 {
		dimension = len(response.Embeddings[0].Vector)
	}
	if dimension == 0 || dimension > 65536 || dimension*len(response.Embeddings) > 4<<20 {
		return fmt.Errorf("%w: embedding dimensions exceed bounds", contentkit.ErrInvalidGeneration)
	}
	for _, embedding := range response.Embeddings {
		if embedding.Index < 0 || embedding.Index >= len(request.Inputs) || len(embedding.Vector) != dimension {
			return fmt.Errorf("%w: inconsistent embedding dimensions", contentkit.ErrInvalidGeneration)
		}
		for _, v := range embedding.Vector {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("%w: non-finite embedding", contentkit.ErrInvalidGeneration)
			}
		}
	}
	return nil
}
