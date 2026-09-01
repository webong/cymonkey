package engineprovider

import (
	"testing"

	grimlockapi "github.com/akive/grimoire/subsystems/grimlock/api/v1alpha1"
)

func TestGrimlockToolsExportsAvailableCapabilities(t *testing.T) {
	tools := GrimlockTools([]EngineDescriptor{
		{Adapter: "browser", Available: true, Capabilities: []string{"dom.click", "dom.read", "dom.read"}},
		{Adapter: "offline", Available: false, Capabilities: []string{"ignored"}},
	})
	if len(tools) != 2 {
		t.Fatalf("expected two unique tools, got %d", len(tools))
	}
	if tools[0].Metadata.Name != "jangolova.browser.dom.click" || tools[0].Spec.Effect != grimlockapi.EffectWrite {
		t.Fatalf("unexpected write tool: %+v", tools[0])
	}
	if tools[1].Metadata.Name != "jangolova.browser.dom.read" || tools[1].Spec.Effect != grimlockapi.EffectRead {
		t.Fatalf("unexpected read tool: %+v", tools[1])
	}
	for _, tool := range tools {
		if err := tool.Validate(); err != nil {
			t.Fatalf("invalid exported tool: %v", err)
		}
	}
}
