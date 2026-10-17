package config

import (
	"strings"
	"testing"
)

func TestExplicitInventory(t *testing.T) {
	for _, input := range []string{`{"devices":[{"id":"a"}]}`, `{"devices":[{"id":"a","endpoint":{"address":"127.0.0.1:5900"},"storage":{"path":"/"}}]}`, `{"devices":[{"id":"a","webrtc":{"video":{"providerId":"missing","deviceId":"desktop"}}}]}`, `{"devices":[],"unknown":true}`} {
		if _, err := Load(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	p, err := Load(strings.NewReader(`{"devices":[{"id":"a","endpoint":{"address":"127.0.0.1:5900"}}]}`))
	if err != nil || len(p) != 1 {
		t.Fatalf("providers %v %v", p, err)
	}
}
