package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"blockade"
)

// ModelTools composes a caller-selected model with an explicitly registered
// tool catalog. Each Generate or Stream performs one model request only.
type ModelTools struct {
	Model blockade.Generator
	Tools *Registry
}

func (m ModelTools) request(request blockade.GenerationRequest) (blockade.GenerationRequest, error) {
	if m.Model == nil || m.Tools == nil {
		return request, errors.New("model and tool registry are required")
	}
	if len(request.Tools) > 0 {
		return request, errors.New("ModelTools supplies tools from its registry")
	}
	for _, definition := range m.Tools.Definitions() {
		request.Tools = append(request.Tools, blockade.ToolDefinition{Name: definition.Name, Description: definition.Description, InputSchema: definition.InputSchema})
	}
	return request, blockade.ValidateGenerationRequest(request)
}

func (m ModelTools) Generate(ctx context.Context, request blockade.GenerationRequest) (blockade.GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return blockade.GenerationResponse{}, err
	}
	request, err := m.request(request)
	if err != nil {
		return blockade.GenerationResponse{}, err
	}
	response, err := m.Model.Generate(ctx, request)
	if err != nil {
		return blockade.GenerationResponse{}, err
	}
	if err := blockade.ValidateGenerationResponse(response); err != nil {
		return blockade.GenerationResponse{}, err
	}
	return response, nil
}

func (m ModelTools) Stream(ctx context.Context, request blockade.GenerationRequest, emit func(blockade.GenerationEvent) error) (blockade.GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return blockade.GenerationResponse{}, err
	}
	request, err := m.request(request)
	if err != nil {
		return blockade.GenerationResponse{}, err
	}
	if emit == nil {
		return blockade.GenerationResponse{}, errors.New("stream callback is required")
	}
	model, ok := m.Model.(blockade.StreamingGenerator)
	if !ok {
		return blockade.GenerationResponse{}, blockade.ErrGenerationUnsupported
	}
	response, err := model.Stream(ctx, request, emit)
	if err != nil {
		return blockade.GenerationResponse{}, err
	}
	if err := blockade.ValidateGenerationResponse(response); err != nil {
		return blockade.GenerationResponse{}, err
	}
	return response, nil
}

// ProposedCalls converts a model response to data that a host can inspect and
// choose to invoke. Conversion does not call the registry.
func ProposedCalls(response blockade.GenerationResponse) ([]Call, error) {
	if err := blockade.ValidateGenerationResponse(response); err != nil {
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
func Continue(messages []blockade.Message, response blockade.GenerationResponse, results []Result) ([]blockade.Message, error) {
	if err := blockade.ValidateGenerationResponse(response); err != nil {
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
	updated := append(append([]blockade.Message(nil), messages...), response.Message)
	for _, call := range response.Message.ToolCalls {
		result, ok := byID[call.ID]
		if !ok || result.Name != call.Name {
			return nil, fmt.Errorf("%w: tool result does not match proposal %q", ErrInvalidResult, call.ID)
		}
		message := blockade.TextMessage("tool", string(result.Output))
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
	var snapshot []blockade.Message
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// FailureResult lets a host resolve a denied or failed proposal explicitly.
// The host chooses the public error text and should redact private details.
func FailureResult(call Call, message string) Result {
	output, _ := json.Marshal(map[string]string{"error": message})
	return Result{ID: call.ID, Name: call.Name, Output: output}
}
