// Command handoff demonstrates Console with a local, scripted generator and
// an in-memory note tool. It contacts no model server and opens no devices.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"console"
	"kit/generationkit"
)

type lookupInput struct {
	Key string `json:"key"`
}

type lookupOutput struct {
	Text  string `json:"text"`
	Found bool   `json:"found"`
}

type report struct {
	Definitions     []console.Definition    `json:"definitions"`
	Proposals       []console.Call          `json:"proposals,omitempty"`
	Result          *console.Result         `json:"result,omitempty"`
	Transcript      []generationkit.Message `json:"transcript,omitempty"`
	ModelRequests   int                     `json:"modelRequests"`
	ToolInvocations int                     `json:"toolInvocations"`
}

func main() {
	action := flag.String("action", "inspect", "inspect, invoke, reject, or direct")
	key := flag.String("key", "console", "key in the example's in-memory notes")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, *action, *key); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, action, key string) error {
	switch action {
	case "inspect", "invoke", "reject", "direct":
	default:
		return errors.New("action must be inspect, invoke, reject, or direct")
	}
	if strings.TrimSpace(key) == "" || len(key) > 128 {
		return errors.New("key must contain 1–128 bytes and be nonempty")
	}
	notes := map[string]string{
		"console":   "The caller selects tools and explicitly requests each model turn.",
		"blockade":  "Model and provider bindings are registered explicitly by the host.",
		"board":     "Device channels retain the caller's grants and limits.",
		"jangolova": "Interface actions use an existing caller-owned connection.",
	}
	var tools console.Registry
	toolInvocations := 0
	lookup, err := console.NewJSONTool(console.Definition{
		Name: "lookup_note", Description: "Read one entry from the example's in-memory notes.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","minLength":1,"maxLength":128}},"required":["key"],"additionalProperties":false}`),
	}, func(ctx context.Context, input lookupInput) (lookupOutput, error) {
		if err := ctx.Err(); err != nil {
			return lookupOutput{}, err
		}
		if strings.TrimSpace(input.Key) == "" || len(input.Key) > 128 {
			return lookupOutput{}, errors.New("key must contain 1–128 bytes and be nonempty")
		}
		toolInvocations++
		text, found := notes[input.Key]
		return lookupOutput{Text: text, Found: found}, nil
	})
	if err != nil {
		return err
	}
	if err := tools.Register(lookup); err != nil {
		return err
	}
	arguments, err := json.Marshal(lookupInput{Key: key})
	if err != nil {
		return err
	}
	output := report{Definitions: tools.Definitions()}
	if action == "direct" {
		// A human or another host can invoke the same tool without a model.
		result, err := tools.Invoke(ctx, console.Call{Name: "lookup_note", Arguments: arguments})
		if err != nil {
			return err
		}
		output.Result = &result
		output.ToolInvocations = toolInvocations
		return printReport(output)
	}

	// This is application-owned example data, not a built-in model catalog.
	generator := &scriptedGenerator{arguments: arguments}
	var models generationkit.ModelCatalog
	if err := models.Register(generationkit.ModelInfo{
		ID: "example-binding", Provider: "local-example", Model: "scripted",
	}, generator); err != nil {
		return err
	}
	composition := console.ModelTools{Model: &models, Tools: &tools}
	messages := []generationkit.Message{generationkit.TextMessage("user", "Look up the note for "+key)}
	response, err := composition.Generate(ctx, generationkit.GenerationRequest{
		Model: "example-binding", Messages: messages,
	})
	if err != nil {
		return err
	}
	proposals, err := console.ProposedCalls(response)
	if err != nil {
		return err
	}
	if len(proposals) != 1 {
		return errors.New("the scripted example must produce exactly one proposal")
	}
	output.Proposals = proposals

	switch action {
	case "invoke", "reject":
		var result console.Result
		if action == "invoke" {
			// The command-line action is the caller's explicit decision.
			result, err = tools.Invoke(ctx, proposals[0])
			if err != nil {
				return err
			}
		} else {
			result = console.FailureResult(proposals[0], "The host declined this call.")
		}
		output.Result = &result
		output.Transcript, err = console.Continue(messages, response, []console.Result{result})
		if err != nil {
			return err
		}
		// The host could submit this transcript later. This example stops here.
	}
	output.ModelRequests = generator.requests
	output.ToolInvocations = toolInvocations
	return printReport(output)
}

func printReport(output report) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// scriptedGenerator supplies a deterministic proposal so the handoff can be
// explored without credentials, network calls, or a paid model. An embedding
// application replaces this value with its configured generationkit.Generator.
type scriptedGenerator struct {
	arguments json.RawMessage
	requests  int
}

func (g *scriptedGenerator) Generate(ctx context.Context, request generationkit.GenerationRequest) (generationkit.GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return generationkit.GenerationResponse{}, err
	}
	g.requests++
	return generationkit.GenerationResponse{
		Model: request.Model, StopReason: "tool_calls",
		Message: generationkit.Message{Role: "assistant", ToolCalls: []generationkit.ToolCall{{
			ID: "example-call", Name: "lookup_note", Arguments: append(json.RawMessage(nil), g.arguments...),
		}}},
	}, nil
}
