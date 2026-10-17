// Package bindings composes explicitly selected SDK capabilities into Console
// tools. It borrows connections and never opens, closes, or expands their scope.
package bindings

import (
	"context"
	"encoding/json"
	"errors"

	"blockade"
	"board"
	"console"
	"jangolova/contract"
	"jangolova/sdk"
)

// Display binds one semantic action on a caller-owned Jangolova connection.
// The host must supply a caller that enforces its per-call approval and policy.
func Display(definition console.Definition, caller sdk.Caller, action contract.Action) (console.Tool, error) {
	if caller == nil || action.Name == "" {
		return console.Tool{}, errors.New("display caller and action name are required")
	}
	return console.NewTool(definition, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		selected := action
		selected.Input = input
		params, err := json.Marshal(selected)
		if err != nil {
			return nil, err
		}
		return caller.Call(ctx, sdk.MethodAct, params)
	})
}

// Device binds a fixed capability and resource to an already granted Board
// connection. Tool arguments cannot change the capability or resource binding.
func Device(definition console.Definition, connection *board.Connection, capability board.Capability, resourceID string) (console.Tool, error) {
	if connection == nil || capability == "" {
		return console.Tool{}, errors.New("device connection and capability are required")
	}
	return console.NewTool(definition, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		result, err := connection.Invoke(ctx, board.Action{Capability: capability, ResourceID: resourceID, Input: input})
		if err != nil {
			return nil, err
		}
		if len(result.Output) == 0 {
			return json.RawMessage(`null`), nil
		}
		return result.Output, nil
	})
}

// Interpret binds a content engine. A host can expose several models as
// distinct tools without giving tool callers access to provider credentials.
func Interpret(definition console.Definition, engine blockade.ContentEngine) (console.Tool, error) {
	if engine == nil {
		return console.Tool{}, errors.New("content engine is required")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, request blockade.InterpretRequest) (blockade.InterpretResponse, error) {
		if request.APIVersion == "" {
			request.APIVersion = blockade.ContentAPIVersion
		}
		if err := blockade.ValidateInterpretRequest(request); err != nil {
			return blockade.InterpretResponse{}, err
		}
		response, err := engine.Interpret(ctx, request)
		if err != nil {
			return blockade.InterpretResponse{}, err
		}
		if err := blockade.ValidateInterpretResponse(response, request); err != nil {
			return blockade.InterpretResponse{}, err
		}
		return response, nil
	})
}
