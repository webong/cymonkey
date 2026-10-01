package displayaction

import (
	"context"
	"os"
	"strings"
	"testing"
)

type recordingRunner struct {
	calls []string
}

func (r *recordingRunner) Run(_ context.Context, command string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, command+" "+strings.Join(args, " "))
	return []byte("png"), nil
}

func (r *recordingRunner) Start(string, ...string) (RunningCommand, error) {
	return fakeRunningCommand{}, nil
}

type fakeRunningCommand struct{}

func (fakeRunningCommand) Signal(os.Signal) error { return nil }
func (fakeRunningCommand) Kill() error            { return nil }
func (fakeRunningCommand) Wait() error            { return nil }

func TestActionsCaptureAndInput(t *testing.T) {
	runner := &recordingRunner{}
	actions := New(Config{OS: "darwin", Runner: runner})
	image, err := actions.Capture(context.Background())
	if err != nil || string(image) != "png" {
		t.Fatalf("capture=%q err=%v", image, err)
	}
	if err := actions.Input(context.Background(), Input{Kind: "text", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 || !strings.HasPrefix(runner.calls[0], "screencapture -x -t png -") || !strings.HasPrefix(runner.calls[1], "osascript -e ") {
		t.Fatalf("native calls=%v", runner.calls)
	}
}

func TestActionsRecordingArgsArePlatformSpecific(t *testing.T) {
	actions := New(Config{OS: "windows"})
	args, err := actions.RecordingArgs("capture.mp4", "mp4")
	if err != nil || strings.Join(args, " ") != "-y -loglevel error -f gdigrab -framerate 30 -i desktop -c:v libx264 capture.mp4" {
		t.Fatalf("recording args=%v err=%v", args, err)
	}
}

func TestActionsLinuxRecordingRequiresDisplay(t *testing.T) {
	actions := New(Config{OS: "linux", LookupEnv: func(string) string { return "" }})
	if _, err := actions.RecordingArgs("capture.webm", "webm"); err == nil {
		t.Fatal("expected DISPLAY requirement")
	}
}
