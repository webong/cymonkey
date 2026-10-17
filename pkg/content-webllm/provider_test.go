package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
)

func TestCapabilitiesDoNotStartBrowser(t *testing.T) {
	p := &provider{descriptor: plugin.Descriptor{Identity: plugin.Identity{ID: "webllm"}}}
	result, err := p.call(context.Background(), "blockade.capabilities", nil)
	if err != nil || p.adapter != nil || result == nil {
		t.Fatalf("result=%#v adapter=%#v err=%v", result, p.adapter, err)
	}
}

func TestStartRequiresModelSetting(t *testing.T) {
	p := &provider{descriptor: plugin.Descriptor{Identity: plugin.Identity{ID: "webllm"}}}
	params, _ := json.Marshal(map[string]any{"id": "browser", "settings": map[string]string{}})
	if _, err := p.call(context.Background(), "blockade.start", params); err == nil {
		t.Fatal("expected missing remote model setting to fail")
	}
}

// Blockade sends "secrets" (empty for an image provider) with every observe
// call, and decodeParams rejects unknown fields, so the provider must declare it.
func TestObserveAcceptsTheSecretsFieldBlockadeSends(t *testing.T) {
	p := &provider{descriptor: plugin.Descriptor{Identity: plugin.Identity{ID: "webllm"}}}
	params := `{"request":{"apiVersion":"blockade.provider-adapter/v1alpha1","request":{"apiVersion":"blockade.observation/v1alpha1","requestId":"r1","image":""}},"secrets":{}}`
	_, err := p.call(context.Background(), "blockade.observe", json.RawMessage(params))
	if err == nil || strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("observe with secrets = %v", err)
	}
}
