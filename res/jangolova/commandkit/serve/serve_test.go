package serve

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"jangolova/commandkit"
)

func call(t *testing.T, runner Runner, request commandkit.Request) (commandkit.Result, error) {
	t.Helper()
	raw, _ := json.Marshal(request)
	out, err := Handler("test.service", runner)(context.Background(), "test.service.command", raw)
	if err != nil {
		return commandkit.Result{}, err
	}
	return out.(commandkit.Result), nil
}

func TestStdoutReturnsWholeStdinReachesTheCommandAndStderrIsKeptWithoutProgress(t *testing.T) {
	result, err := call(t, func(_ context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
		input, _ := io.ReadAll(stdin)
		io.WriteString(stdout, strings.Join(args, ",")+"|"+string(input))
		io.WriteString(stderr, "log line\n")
		return nil
	}, commandkit.Request{Args: []string{"a", "b"}, Stdin: []byte("in")})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Stdout) != "a,b|in" || result.Error != "" {
		t.Fatalf("result = %+v", result)
	}
	// No progress reporter exists in this test, so the log text is returned.
	if string(result.Stderr) != "log line\n" {
		t.Fatalf("stderr = %q", result.Stderr)
	}
}

func TestACommandFailureKeepsItsOutput(t *testing.T) {
	result, err := call(t, func(_ context.Context, _ []string, _ io.Reader, stdout, _ io.Writer) error {
		io.WriteString(stdout, "partial")
		return errors.New("boom")
	}, commandkit.Request{})
	if err != nil || string(result.Stdout) != "partial" || result.Error != "boom" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestOtherMethodsAndOversizedInputAreRefused(t *testing.T) {
	runner := func(context.Context, []string, io.Reader, io.Writer, io.Writer) error { return nil }
	if _, err := Handler("test.service", runner)(context.Background(), "test.service.shell", json.RawMessage(`{}`)); err == nil {
		t.Fatal("another operation was served")
	}
	if _, err := call(t, runner, commandkit.Request{Stdin: make([]byte, commandkit.MaxStdinBytes+1)}); err == nil {
		t.Fatal("oversized input was accepted")
	}
}

func TestStdoutBeyondTheBoundIsTruncatedAndReported(t *testing.T) {
	var b bounded
	chunk := make([]byte, commandkit.MaxOutputBytes/2+1)
	b.Write(chunk)
	b.Write(chunk)
	if !b.over || b.buffer.Len() != commandkit.MaxOutputBytes {
		t.Fatalf("over=%v len=%d", b.over, b.buffer.Len())
	}
}
