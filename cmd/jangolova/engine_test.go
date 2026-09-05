package main

import (
	"strings"
	"testing"
)

func TestDecodeEngineOptions(t *testing.T) {
	t.Parallel()

	raw, err := decodeEngineOptions(`{"headless":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"headless":true}` {
		t.Fatalf("options = %s", raw)
	}
	for _, invalid := range []string{"", "null", "[]", "not-json"} {
		if _, err := decodeEngineOptions(invalid); err == nil {
			t.Fatalf("decodeEngineOptions(%q) succeeded", invalid)
		}
	}
}

func TestEndpointFlags(t *testing.T) {
	t.Parallel()

	var values endpointFlags
	if err := values.Set("cdp=http://127.0.0.1:9222"); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Protocol != "cdp" || values[0].URL != "http://127.0.0.1:9222" {
		t.Fatalf("endpoints = %#v", values)
	}
	for _, invalid := range []string{"cdp", "=value", "bad/path=http://localhost"} {
		if err := values.Set(invalid); err == nil {
			t.Fatalf("endpoint %q was accepted", invalid)
		}
	}
	if got := values.String(); !strings.Contains(got, "cdp") {
		t.Fatalf("String() = %q", got)
	}
}

func TestHandleFlags(t *testing.T) {
	t.Parallel()

	var values handleFlags
	if err := values.Set("native.window=window-1234"); err != nil {
		t.Fatal(err)
	}
	if values["native.window"] != "window-1234" {
		t.Fatalf("handles = %#v", values)
	}
	for _, invalid := range []string{
		"native.window",
		"native/window=value",
		"native.window=",
	} {
		if err := values.Set(invalid); err == nil {
			t.Fatalf("handle %q was accepted", invalid)
		}
	}
}
