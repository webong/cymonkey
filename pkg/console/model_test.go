package console

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"kit/generationkit"
)

type proposingModel struct {
	count   int
	request generationkit.GenerationRequest
}

func (m *proposingModel) Generate(_ context.Context, request generationkit.GenerationRequest) (generationkit.GenerationResponse, error) {
	m.count++
	m.request = request
	return generationkit.GenerationResponse{Model: request.Model, Message: generationkit.Message{Role: "assistant", ToolCalls: []generationkit.ToolCall{{ID: "chosen", Name: "lookup", Arguments: json.RawMessage(`{"key":"a"}`)}}}}, nil
}

func TestModelCompositionRequiresExplicitToolExecutionAndContinuation(t *testing.T) {
	var registry Registry
	invoked := 0
	tool, err := NewJSONTool(Definition{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(_ context.Context, input struct {
		Key string `json:"key"`
	}) (map[string]string, error) { invoked++; return map[string]string{"value": input.Key}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	model := &proposingModel{}
	composition := ModelTools{Model: model, Tools: &registry}
	messages := []generationkit.Message{generationkit.TextMessage("user", "find a")}
	response, err := composition.Generate(context.Background(), generationkit.GenerationRequest{Model: "host-choice", Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	if model.count != 1 || invoked != 0 || len(model.request.Tools) != 1 {
		t.Fatal("one model turn must only advertise tools")
	}
	calls, err := ProposedCalls(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || invoked != 0 {
		t.Fatal("proposal conversion executed a tool")
	}
	result, err := registry.Invoke(context.Background(), calls[0])
	if err != nil {
		t.Fatal(err)
	}
	continued, err := Continue(messages, response, []Result{result})
	if err != nil {
		t.Fatal(err)
	}
	if invoked != 1 || model.count != 1 || len(continued) != 3 || continued[2].ToolCallID != "chosen" || continued[2].ToolName != "lookup" {
		t.Fatal("unexpected explicit continuation")
	}
	continued[1].ToolCalls[0].Arguments[0] = '!'
	if response.Message.ToolCalls[0].Arguments[0] != '{' {
		t.Fatal("continuation aliases original transcript")
	}
	result.ID = "wrong"
	if _, err = Continue(messages, response, []Result{result}); err == nil {
		t.Fatal("mismatched result accepted")
	}
}

func TestCanceledInvocationDoesNotExecuteHandler(t *testing.T) {
	var registry Registry
	invoked := false
	tool, err := NewTool(Definition{Name: "work", InputSchema: json.RawMessage(`{}`)}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		invoked = true
		return json.RawMessage(`null`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = registry.Invoke(ctx, Call{Name: "work", Arguments: json.RawMessage(`{}`)}); !errors.Is(err, context.Canceled) || invoked {
		t.Fatal("canceled call reached handler")
	}
}
