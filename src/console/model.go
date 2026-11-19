package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"jangolova/contentkit"

	"github.com/webong/ext/res/int/inference"
)

// ModelTools composes a caller-selected model with an explicitly registered
// tool catalog. Each Generate or Stream performs one model request only.
type ModelTools struct {
	Model inference.Generator
	Tools *Registry
}

func (m ModelTools) request(request inference.GenerationRequest) (inference.GenerationRequest, error) {
	if m.Model == nil || m.Tools == nil {
		return request, errors.New("model and tool registry are required")
	}
	if len(request.Tools) > 0 {
		return request, errors.New("ModelTools supplies tools from its registry")
	}
	for _, definition := range m.Tools.Definitions() {
		request.Tools = append(request.Tools, inference.ToolDefinition{Name: definition.Name, Description: definition.Description, InputSchema: definition.InputSchema})
	}
	return request, request.Validate()
}

func (m ModelTools) Generate(ctx context.Context, request inference.GenerationRequest) (inference.GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return inference.GenerationResponse{}, err
	}
	request, err := m.request(request)
	if err != nil {
		return inference.GenerationResponse{}, err
	}
	response, err := m.Model.Generate(ctx, request)
	if err != nil {
		return inference.GenerationResponse{}, err
	}
	if err := ValidateResult(response, request); err != nil {
		return inference.GenerationResponse{}, err
	}
	return response, nil
}

func (m ModelTools) Stream(ctx context.Context, request inference.GenerationRequest, sink inference.EventSink) (inference.GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return inference.GenerationResponse{}, err
	}
	request, err := m.request(request)
	if err != nil {
		return inference.GenerationResponse{}, err
	}
	if sink == nil {
		return inference.GenerationResponse{}, errors.New("stream callback is required")
	}
	model, ok := m.Model.(inference.Streamer)
	if !ok {
		return inference.GenerationResponse{}, contentkit.ErrGenerationUnsupported
	}
	response, err := model.Stream(ctx, request, sink)
	if err != nil {
		return inference.GenerationResponse{}, err
	}
	if err := ValidateResult(response, request); err != nil {
		return inference.GenerationResponse{}, err
	}
	return response, nil
}

// ProposedCalls converts a model response to data that a host can inspect and
// choose to invoke. Conversion does not call the registry.
func ProposedCalls(response inference.GenerationResponse) ([]Call, error) {
	if err := response.Validate(); err != nil {
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
func Continue(messages []inference.Message, response inference.GenerationResponse, results []Result) ([]inference.Message, error) {
	if err := response.Validate(); err != nil {
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
	updated := append(append([]inference.Message(nil), messages...), response.Message)
	for _, call := range response.Message.ToolCalls {
		result, ok := byID[call.ID]
		if !ok || result.Name != call.Name {
			return nil, fmt.Errorf("%w: tool result does not match proposal %q", ErrInvalidResult, call.ID)
		}
		message := TextMessage(inference.RoleTool, string(result.Output))
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
	var snapshot []inference.Message
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
