package devices

import (
	"board/displayaction"
	"context"
	"errors"
	"kit/devicekit"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type desktopRunner struct {
	mu    sync.Mutex
	calls []string
}

func (r *desktopRunner) Run(_ context.Context, command string, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, command)
	r.mu.Unlock()
	return []byte("image"), nil
}
func (r *desktopRunner) Start(command string, args ...string) (displayaction.RunningCommand, error) {
	r.mu.Lock()
	r.calls = append(r.calls, command+" "+strings.Join(args, " "))
	r.mu.Unlock()
	if err := os.WriteFile(args[len(args)-1], []byte("video"), 0600); err != nil {
		return nil, err
	}
	return &recordingProcess{done: make(chan struct{})}, nil
}

type recordingProcess struct {
	done chan struct{}
	once sync.Once
}

func (p *recordingProcess) Signal(os.Signal) error { p.once.Do(func() { close(p.done) }); return nil }
func (p *recordingProcess) Kill() error            { return p.Signal(os.Interrupt) }
func (p *recordingProcess) Wait() error            { <-p.done; return nil }
func TestDesktopGrantRecordingExpiryAndCleanup(t *testing.T) {
	ctx := context.Background()
	runner := &desktopRunner{}
	dir := t.TempDir()
	p, err := Desktop(DesktopOptions{ID: "screen", Scope: "desktop", OS: "darwin", Capture: true, Input: true, Recording: true, RecordingDirectory: dir, MaxRecordingDuration: 20 * time.Millisecond, Runner: runner, LookPath: func(name string) (string, error) { return name, nil }})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := devicekit.NewRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	c, err := registry.Open(ctx, devicekit.OpenRequest{ProviderID: "screen", DeviceID: "desktop", Grant: devicekit.Grant{Capabilities: []devicekit.Capability{devicekit.DisplayCapture, devicekit.RecordingStart, devicekit.RecordingStop, RecordingRead}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	if _, err = c.Invoke(ctx, devicekit.Action{Capability: devicekit.KeyboardPress, Input: []byte(`{"key":"a"}`)}); !errors.Is(err, devicekit.ErrNotGranted) {
		t.Fatal(err)
	}
	image, err := c.Stream(ctx, devicekit.Action{Capability: devicekit.DisplayCapture})
	if err != nil {
		t.Fatal(err)
	}
	image.Close()
	if _, err = c.Invoke(ctx, devicekit.Action{Capability: devicekit.RecordingStart, Input: []byte(`{"format":"webm"}`)}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		events, err := c.Events(0)
		if err != nil {
			t.Fatal(err)
		}
		complete := false
		for _, e := range events.Events {
			complete = complete || e.Type == "recording.completed"
		}
		if complete {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recording did not expire")
		}
		time.Sleep(time.Millisecond)
	}
	content, err := c.Stream(ctx, devicekit.Action{Capability: RecordingRead})
	if err != nil {
		t.Fatal(err)
	}
	data, err := devicekit.ReadAll(content, 1024)
	content.Close()
	if err != nil || string(data) != "video" {
		t.Fatalf("recording %q %v", data, err)
	}
	if err = c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) > 0 {
		t.Fatal("recording artifacts survived close")
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.calls) != 2 || !strings.Contains(runner.calls[1], "-fs 134217728") {
		t.Fatalf("unexpected commands %v", runner.calls)
	}
}
func TestWaylandDoesNotAdvertiseUnavailableVideo(t *testing.T) {
	_, err := Desktop(DesktopOptions{ID: "screen", Scope: "desktop", OS: "linux", Video: true, Environment: map[string]string{"DISPLAY": "", "WAYLAND_DISPLAY": "wayland-0"}, LookPath: func(s string) (string, error) { return s, nil }})
	if err == nil {
		t.Fatal("accepted unsupported capture backend")
	}
}
