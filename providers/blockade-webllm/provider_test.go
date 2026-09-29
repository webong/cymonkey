package main

import (
	"context"
	"encoding/json"
	"testing"

	"providerplugin"
)

func TestCapabilitiesDoNotStartBrowser(t *testing.T) {
	p := &provider{manifest: providerplugin.Manifest{APIVersion: providerplugin.APIVersion, Name: "webllm", Contracts: []providerplugin.Contract{{Name: blockadeContract, Version: blockadeContractVersion}}}}
	result, err := p.call(context.Background(), "blockade.capabilities", nil)
	if err != nil || p.adapter != nil || result == nil {
		t.Fatalf("result=%#v adapter=%#v err=%v", result, p.adapter, err)
	}
}

func TestStartRequiresModelSetting(t *testing.T) {
	p := &provider{manifest: providerplugin.Manifest{APIVersion: providerplugin.APIVersion, Name: "webllm", Contracts: []providerplugin.Contract{{Name: blockadeContract, Version: blockadeContractVersion}}}}
	params, _ := json.Marshal(map[string]any{"id": "browser", "settings": map[string]string{}})
	if _, err := p.call(context.Background(), "blockade.start", params); err == nil {
		t.Fatal("expected missing remote model setting to fail")
	}
}
