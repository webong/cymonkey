package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"jangolova/contentkit"

	"jangolova/generationkit"
)

// ModelTools composes a caller-selected model with an explicitly registered
// tool catalog. Each Generate or Stream performs one model request only.
type ModelTools struct {
	Model generationkit.Generator
	Tools *Registry
}

func (m ModelTools) request(request generationkit.GenerationRequest) (generationkit.GenerationRequest, error) {
	if m.Model == nil || m.Tools == nil {
		return request, errors.New("model and tool registry are required")
	}
	if len(request.Tools) > 0 {
		return request, errors.New("ModelTools supplies tools from its registry")
	}
	for _, definition := range m.Tools.Definitions() {
		request.Tools = append(request.Tools, generationkit.ToolDefinition{Name: definition.Name, Description: definition.Description, InputSchema: definition.InputSchema})
	}
	return request, generationkit.ValidateGenerationRequest(request)
}

func (m ModelTools) Generate(ctx context.Context, request generationkit.GenerationRequest) (generationkit.GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	request, err := m.request(request)
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	response, err := m.Model.Generate(ctx, request)
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	if err := generationkit.ValidateGenerationResult(response, request); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	return response, nil
}

func (m ModelTools) Stream(ctx context.Context, request generationkit.GenerationRequest, emit func(generationkit.GenerationEvent) error) (generationkit.GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	request, err := m.request(request)
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	if emit == nil {
		return generationkit.GenerationResponse{}, errors.New("stream callback is required")
	}
	model, ok := m.Model.(generationkit.StreamingGenerator)
	if !ok {
		return generationkit.GenerationResponse{}, contentkit.ErrGenerationUnsupported
	}
	response, err := model.Stream(ctx, request, emit)
	if err != nil {
		return generationkit.GenerationResponse{}, err
	}
	if err := generationkit.ValidateGenerationResult(response, request); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	return response, nil
}

// ProposedCalls converts a model response to data that a host can inspect and
// choose to invoke. Conversion does not call the registry.
func ProposedCalls(response generationkit.GenerationResponse) ([]Call, error) {
	if err := generationkit.ValidateGenerationResponse(response); err != nil {
		return nil, err
	}
	calls := make([]Call, 0, len(response.Message.ToolCalls))
	for _, call := range response.Message.ToolCalls {
		calls = append(calls, Call{ID: call.ID, Name: call.Name, Arguments: append(json.RawMessage(nil), call.Arguments...)})
	}
	return calls, nil
}

// Continue builds a new transcript after the caller has resolved every
// proposed call. Results are reordered to the proposals and must match IDs
// and names. Nothing is executed and no subsequent model request is started.
func Continue(messages []generationkit.Message, response generationkit.GenerationResponse, results []Result) ([]generationkit.Message, error) {
	if err := generationkit.ValidateGenerationResponse(response); err != nil {
		return nil, err
	}
	if len(results) != len(response.Message.ToolCalls) {
		return nil, errors.New("each proposed call needs one result before continuing")
	}
	byID := map[string]Result{}
	for _, result := range results {
		if _, exists := byID[result.ID]; exists || result.ID == "" || !json.Valid(result.Output) {
			return nil, ErrInvalidResult
		}
		byID[result.ID] = result
	}
	updated := append(append([]generationkit.Message(nil), messages...), response.Message)
	for _, call := range response.Message.ToolCalls {
		result, ok := byID[call.ID]
		if !ok || result.Name != call.Name {
			return nil, fmt.Errorf("%w: tool result does not match proposal %q", ErrInvalidResult, call.ID)
		}
		message := generationkit.TextMessage("tool", string(result.Output))
		message.ToolError = result.IsError
		message.ToolCallID = call.ID
		message.ToolName = call.Name
		updated = append(updated, message)
	}
	// Copy nested bytes as well as the outer slice so a host can retain both
	// transcript versions while it updates the next request.
	data, err := json.Marshal(updated)
	if err != nil {
		return nil, err
	}
	var snapshot []generationkit.Message
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// FailureResult lets a host resolve a denied or failed proposal explicitly.
// The host chooses the public error text and should redact private details.
func FailureResult(call Call, message string) Result {
	output, _ := json.Marshal(map[string]string{"error": message})
	return Result{ID: call.ID, Name: call.Name, Output: output, IsError: true}
}
