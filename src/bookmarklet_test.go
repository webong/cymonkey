package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBookmarkletCLI(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.js")
	input := "document.title = 'Café + 100%'; // comment\n"
	if err := os.WriteFile(source, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"bookmarklet", "export", "--source", source}, &out, &out); err != nil {
		t.Fatal(err)
	}
	urlFile := filepath.Join(dir, "bookmark.txt")
	if err := os.WriteFile(urlFile, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"bookmarklet", "import", "--source", urlFile}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != input {
		t.Fatalf("source changed: %q", out.String())
	}
	out.Reset()
	if err := bookmarkletCommand([]string{"export", "--source", source, "--format", "html", "--name", "Highlight"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `draggable="true"`) {
		t.Fatal("missing install link")
	}
	for _, args := range [][]string{nil, {"run"}, {"export"}, {"export", "--source", source, "--format", "bad"}, {"import", "--source", source}, {"export", "--source", source, "extra"}} {
		out.Reset()
		if err := bookmarkletCommand(args, &out); err == nil {
			t.Fatalf("accepted %v", args)
		}
		if out.Len() != 0 {
			t.Fatal("failed preparation wrote partial output")
		}
	}
}
