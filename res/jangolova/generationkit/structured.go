package generationkit

import (
	"context"
	"encoding/json"
	"fmt"
	"jangolova/contentkit"
	"strings"

	"github.com/webong/ext/pkg/plugin-go/contract/validate/jsonschema"
)

// StructuredEvent exposes unmodified text deltas and complete schema-valid
// candidates. Candidate objects may change before completion. Missing fields
// and incomplete JSON are never repaired or presented as validated objects.
type StructuredEvent[T any] struct {
	Kind   string `json:"kind"` // text.delta, candidate, completed
	Text   string `json:"text,omitempty"`
	Object *T     `json:"object,omitempty"`
}

// StreamStructuredOutput makes one explicitly selected model request. The
// returned object is authoritative; callbacks cannot cause another model turn.
func StreamStructuredOutput[T any](ctx context.Context, model StreamingGenerator, request GenerationRequest, emit func(StructuredEvent[T]) error) (T, GenerationResponse, error) {
	var zero T
	if model == nil || emit == nil || len(request.OutputSchema) == 0 {
		return zero, GenerationResponse{}, fmt.Errorf("%w: structured streaming needs a model, schema and consumer", contentkit.ErrInvalidGeneration)
	}
	if err := ValidateGenerationRequest(request); err != nil {
		return zero, GenerationResponse{}, err
	}
	schema, err := jsonschema.Compile(request.OutputSchema)
	if err != nil {
		return zero, GenerationResponse{}, err
	}
	var text strings.Builder
	var inspected int64
	response, err := model.Stream(ctx, request, func(event GenerationEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if event.Kind != "text.delta" {
			return nil
		}
		if text.Len()+len(event.Text) > 32<<20 {
			return fmt.Errorf("%w: structured output exceeds byte ceiling", contentkit.ErrInvalidGeneration)
		}
		text.WriteString(event.Text)
		if err := emit(StructuredEvent[T]{Kind: "text.delta", Text: event.Text}); err != nil {
			return err
		}
		// Candidate updates are advisory; cap cumulative parsing work.
		inspected += int64(text.Len())
		if inspected > 64<<20 {
			return nil
		}
		data := json.RawMessage(text.String())
		if !json.Valid(data) || schema.Validate(data) != nil {
			return nil
		}
		var candidate T
		if json.Unmarshal(data, &candidate) != nil {
			return nil
		}
		return emit(StructuredEvent[T]{Kind: "candidate", Object: &candidate})
	})
	if err != nil {
		return zero, response, err
	}
	value, err := DecodeStructuredOutput[T](response, request.OutputSchema)
	if err != nil {
		return zero, response, err
	}
	// Decode again for the callback so its mutation cannot alter the return value.
	copy, err := DecodeStructuredOutput[T](response, request.OutputSchema)
	if err != nil {
		return zero, response, err
	}
	if err := emit(StructuredEvent[T]{Kind: "completed", Object: &copy}); err != nil {
		return zero, response, err
	}
	return value, response, nil
}

// ValidateStructuredOutput checks completed text output. It never repairs data
// or makes another request. Refusals, truncation and tool proposals are not objects.
func ValidateStructuredOutput(response GenerationResponse, schema json.RawMessage) error {
	if err := ValidateGenerationResponse(response); err != nil {
		return err
	}
	completed := response.StopReason == "" || response.StopReason == "stop" || response.StopReason == "completed"
	for _, part := range response.Message.Parts {
		if part.Kind == "refusal" {
			completed = false
		}
	}
	if len(response.Message.ToolCalls) != 0 || !completed {
		return fmt.Errorf("%w: response is not completed structured output", contentkit.ErrInvalidGeneration)
	}
	return jsonschema.Validate(schema, json.RawMessage(response.Text()))
}

func DecodeStructuredOutput[T any](response GenerationResponse, schema json.RawMessage) (T, error) {
	var value T
	if err := ValidateStructuredOutput(response, schema); err != nil {
		return value, err
	}
	err := json.Unmarshal([]byte(response.Text()), &value)
	return value, err
}
