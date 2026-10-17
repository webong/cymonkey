package main

import (
	"bytes"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPublicCommandsHideInternalLibraries(t *testing.T) {
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(t.TempDir(), "plugins"))
	for _, command := range []string{"board", "blockade", "provider"} {
		if err := run([]string{command, "help"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("internal command %q is public", command)
		}
	}
	for _, command := range []string{"device", "display", "modules", "plugins"} {
		var stdout, stderr bytes.Buffer
		if err := run([]string{command, "help"}, &stdout, &stderr); err != nil {
			t.Fatalf("%s help: %v", command, err)
		}
		text := strings.ToLower(stdout.String() + stderr.String())
		if internal := regexp.MustCompile(`\b(board|blockade|jangolova)\b`).FindString(text); internal != "" {
			t.Fatalf("%s help exposes %q: %s", command, internal, text)
		}
	}
	var rootHelp bytes.Buffer
	if err := run([]string{"help"}, io.Discard, &rootHelp); err != nil {
		t.Fatal(err)
	}
	if internal := regexp.MustCompile(`\b(board|blockade|jangolova)\b`).FindString(strings.ToLower(rootHelp.String())); internal != "" {
		t.Fatalf("root help exposes %q: %s", internal, rootHelp.String())
	}
}

func TestDeviceJSONUsesCymonkeyContract(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"device", "devices", "--root", "photos=" + t.TempDir(), "--json"}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "providerId") || !strings.Contains(output.String(), `"apiVersion":"cymonkey.device/v1alpha1"`) || !strings.Contains(output.String(), `"adapterId":`) {
		t.Fatalf("public device inventory = %s", output.String())
	}
}
