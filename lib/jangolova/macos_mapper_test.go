package cymonkey

import (
	"encoding/json"
	"testing"
)

func TestMacOSMapperCombinesAppleEventsAndAccessibility(t *testing.T) {
	mapping, err := MapMacOSPrimitives([]MacOSPrimitive{
		{Kind: "apple-event-command", BundleID: "com.apple.Music", Name: "play", AccessGroup: "com.apple.Music.playback", InputSchema: json.RawMessage(`{"type":"object","required":["surfaceId"]}`)},
		{Kind: "accessibility-query", BundleID: "com.apple.Music", InputSchema: objectSchema("surfaceId", "selector")},
		{Kind: "accessibility-action", BundleID: "com.apple.Music", InputSchema: objectSchema("surfaceId", "elementId", "action")},
		{Kind: "accessibility-attribute", BundleID: "com.apple.Music", Settable: true, InputSchema: objectSchema("surfaceId", "elementId", "attribute", "value")},
	}, []string{"com.apple.Music"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(mapping.Capabilities))
	for _, capability := range mapping.Capabilities {
		names = append(names, capability.Name)
	}
	for _, expected := range []string{"app.command.invoke", "app.command.list", "ui.action.invoke", "ui.attribute.set", "ui.query"} {
		if !contains(names, expected) {
			t.Fatalf("mapped capabilities %v omit %s", names, expected)
		}
	}
	if len(mapping.Commands["com.apple.Music"]) != 1 || mapping.Commands["com.apple.Music"][0] != "play" {
		t.Fatalf("commands = %#v", mapping.Commands)
	}
}

func TestMacOSMapperRejectsRawScriptAndSystemWideTree(t *testing.T) {
	for _, kind := range []string{"applescript", "raw-apple-event", "system-accessibility-tree"} {
		if _, err := MapMacOSPrimitives([]MacOSPrimitive{{Kind: kind, BundleID: "com.example.App"}}, nil, nil); err == nil {
			t.Fatalf("MapMacOSPrimitives accepted %s", kind)
		}
	}
}

func TestMacOSMapperFiltersBundleAndCapabilityPolicy(t *testing.T) {
	mapping, err := MapMacOSPrimitives([]MacOSPrimitive{
		{Kind: "accessibility-query", BundleID: "com.allowed.App"},
		{Kind: "accessibility-action", BundleID: "com.denied.App"},
	}, []string{"com.allowed.App"}, []string{"ui.query"})
	if err != nil {
		t.Fatal(err)
	}
	if len(mapping.Capabilities) != 1 || mapping.Capabilities[0].Name != "ui.query" {
		t.Fatalf("capabilities = %#v", mapping.Capabilities)
	}
}
