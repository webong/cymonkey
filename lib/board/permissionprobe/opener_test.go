package permissionprobe

import (
	"context"
	"errors"
	"testing"
)

type recordingRunner struct {
	command string
	args    []string
	err     error
}

func (r *recordingRunner) Run(_ context.Context, command string, args ...string) ([]byte, error) {
	r.command = command
	r.args = append([]string(nil), args...)
	return nil, r.err
}

func TestOpenWindowsCapturePermission(t *testing.T) {
	runner := &recordingRunner{}
	result, err := New(Config{OS: "windows", Runner: runner}).Open(context.Background(), "windows.screen-capture")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !result.Opened || result.Permission != "windows.screen-capture" || runner.command != "cmd" {
		t.Fatalf("unexpected result=%+v command=%q args=%v", result, runner.command, runner.args)
	}
}

func TestOpenLinuxX11DoesNotInvokeRunner(t *testing.T) {
	runner := &recordingRunner{}
	result, err := New(Config{OS: "linux", Runner: runner}).Open(context.Background(), "linux.x11")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if result.Opened || runner.command != "" || result.Message == "" {
		t.Fatalf("unexpected result=%+v command=%q", result, runner.command)
	}
}

func TestOpenRejectsUnsupportedPermission(t *testing.T) {
	_, err := New(Config{OS: "darwin", Runner: &recordingRunner{}}).Open(context.Background(), "windows.screen-capture")
	if err == nil {
		t.Fatal("expected unsupported permission error")
	}
}

func TestOpenRedactsCommandFailure(t *testing.T) {
	runner := &recordingRunner{err: errors.New("/private/secret/settings details")}
	_, err := New(Config{OS: "windows", Runner: runner}).Open(context.Background(), "windows.screen-capture")
	if err == nil || err.Error() != "open permission settings failed" {
		t.Fatal("expected command failure")
	}
}
