package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestDeviceCommandUsesPublicCLI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"device", "help"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "drive-read") || !strings.Contains(stderr.String(), "keyboard-press") {
		t.Fatalf("device help output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if regexp.MustCompile(`(?i)\bboard\b`).MatchString(stderr.String()) {
		t.Fatalf("device help exposes internal library: %q", stderr.String())
	}
	if err := run([]string{"board", "help"}, &stdout, &stderr); err == nil {
		t.Fatal("internal library name unexpectedly accepted as a public command")
	}
}
