package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestBoardCommandUsesLibraryCLI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"board", "help"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "drive-read") || !strings.Contains(stderr.String(), "keyboard-press") {
		t.Fatalf("board help output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
