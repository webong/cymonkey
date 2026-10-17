package blockade

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type privateGenerator struct {
	called   int
	request  GenerationRequest
	response GenerationResponse
}

func (g *privateGenerator) Generate(_ context.Context, r GenerationRequest) (GenerationResponse, error) {
	g.called++
	g.request = r
	if g.response.Model != "" {
		return g.response, nil
	}
	return GenerationResponse{Model: r.Model, Message: TextMessage("assistant", "done")}, nil
}

func TestModelCatalogAcceptsOnlyExplicitArbitraryBindings(t *testing.T) {
	var catalog ModelCatalog
	if len(catalog.Models()) != 0 {
		t.Fatal("catalog must start empty")
	}
	request := GenerationRequest{Model: "tenant-choice", Messages: []Message{TextMessage("user", "hello")}}
	if _, err := catalog.Generate(context.Background(), request); err == nil {
		t.Fatal("unregistered binding should fail")
	}
	provider := &privateGenerator{}
	info := ModelInfo{ID: "tenant-choice", Provider: "private-pipeline", Model: "vendor-model-that-core-does-not-know", Capabilities: []string{"custom.generate"}}
	if err := catalog.Register(info, provider); err != nil {
		t.Fatal(err)
	}
	info.Capabilities[0] = "changed"
	if catalog.Models()[0].Capabilities[0] != "custom.generate" {
		t.Fatal("metadata was not copied")
	}
	if _, err := catalog.Generate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if provider.called != 1 || provider.request.Model != "vendor-model-that-core-does-not-know" {
		t.Fatalf("wrong dispatch: %+v", provider)
	}
	if _, err := catalog.Stream(context.Background(), request, func(GenerationEvent) error { return nil }); !errors.Is(err, ErrGenerationUnsupported) {
		t.Fatalf("streaming must not be simulated: %v", err)
	}
}

func TestGenerationRejectsMalformedProviderToolProposals(t *testing.T) {
	response := GenerationResponse{Model: "private", Message: Message{Role: "assistant", ToolCalls: []ToolCall{{ID: "call-1", Name: "operation", Arguments: json.RawMessage(`[]`)}}}}
	if !errors.Is(ValidateGenerationResponse(response), ErrInvalidGeneration) {
		t.Fatal("tool arguments must be objects")
	}
	response.Message.ToolCalls[0].Arguments = json.RawMessage(`{}`)
	response.Message.ToolCalls = append(response.Message.ToolCalls, response.Message.ToolCalls[0])
	if ValidateGenerationResponse(response) == nil {
		t.Fatal("duplicate call IDs must fail")
	}
	provider := &privateGenerator{response: response}
	var catalog ModelCatalog
	if err := catalog.Register(ModelInfo{ID: "private", Provider: "arbitrary"}, provider); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Generate(context.Background(), GenerationRequest{Model: "private", Messages: []Message{TextMessage("user", "go")}}); err == nil {
		t.Fatal("catalog must reject malformed provider response")
	}
}
