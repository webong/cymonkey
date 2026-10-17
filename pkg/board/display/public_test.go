package display_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	displayengine "board/display"
	"jangolova/displaykit/contract"
	"jangolova/displaykit/sdk"
)

// This backend deliberately uses only public signatures. A Lens Studio or
// other contributor module can implement the same interface out of tree.
type contributedBackend struct{ calls *int }

func (contributedBackend) Name() displayengine.BackendName { return "contributed-mcp" }
func (contributedBackend) Domains() []contract.Domain {
	return []contract.Domain{contract.DomainRender}
}
func (contributedBackend) Compatible(t sdk.EngineTarget) bool {
	_, ok := t.Endpoint("mcp-streamable-http")
	return t.Kind == "contributed-editor" && ok
}
func (b contributedBackend) Connect(_ context.Context, _ sdk.EngineSpec, _ sdk.EngineTarget, _ displayengine.Options) (sdk.EngineInstance, error) {
	*b.calls++
	return publicSession{}, nil
}

type publicSession struct{}

func (publicSession) Disconnect(context.Context) error { return nil }
func (publicSession) Authorize(context.Context, sdk.AuthorizeRequest) (sdk.AuthorizeDecision, error) {
	return sdk.AuthorizeDecision{}, nil
}
func TestContributedBackendUsesOnlyPublicContract(t *testing.T) {
	calls := 0
	a := displayengine.Adapter{Backends: []displayengine.Backend{contributedBackend{&calls}}}
	_, err := a.Connect(context.Background(), sdk.EngineSpec{Options: json.RawMessage(`{"domain":"render","driver":"contributed-mcp"}`)}, sdk.EngineTarget{Kind: "contributed-editor", Endpoints: []sdk.TargetEndpoint{{Protocol: "mcp-streamable-http", URL: "http://127.0.0.1:1"}}})
	if err != nil || calls != 1 {
		t.Fatalf("connect: %v, calls: %d", err, calls)
	}
}
func TestPublicIntegrationHasNoPrivateDependencies(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "board/display", "jangolova/displaykit/registry").CombinedOutput()
	if err != nil {
		t.Fatalf("dependency graph: %v: %s", err, out)
	}
	for _, p := range strings.Fields(string(out)) {
		if strings.HasPrefix(p, "cymonkey/src/internal/") || strings.HasPrefix(p, "cymonkey/src/adapters/") {
			t.Errorf("private host dependency in public integration: %s", p)
		}
	}
}
