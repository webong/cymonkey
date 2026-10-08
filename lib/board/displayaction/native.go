// Package displayaction contains native desktop capture, input, and recording
// mechanics. The caller authorizes the desktop session and supplies a bounded
// CommandRunner when integrating with a host permission system.
package displayaction

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const windowsScreenshotCommand = `Add-Type -AssemblyName System.Drawing; Add-Type -AssemblyName System.Windows.Forms; $b = New-Object System.Drawing.Bitmap([System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Width, [System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Height); $g = [System.Drawing.Graphics]::FromImage($b); $g.CopyFromScreen([System.Drawing.Point]::Empty, [System.Drawing.Point]::Empty, $b.Size); $m = New-Object System.IO.MemoryStream; $b.Save($m, [System.Drawing.Imaging.ImageFormat]::Png); [Console]::OpenStandardOutput().Write($m.ToArray(), 0, $m.Length)`

// RunningCommand is the lifecycle seam for a native recording process.
type RunningCommand interface {
	Signal(os.Signal) error
	Kill() error
	Wait() error
}

// CommandRunner is the only process boundary needed by the display actions.
type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
	Start(string, ...string) (RunningCommand, error)
}

// ExecCommandRunner is the native development runner. Command output remains
// local to the caller and is never included in the typed display contract.
type ExecCommandRunner struct{}

func (ExecCommandRunner) Run(ctx context.Context, command string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, command, args...).Output()
}

func (ExecCommandRunner) Start(command string, args ...string) (RunningCommand, error) {
	process := exec.Command(command, args...)
	process.Stdout = nil
	process.Stderr = nil
	if err := process.Start(); err != nil {
		return nil, err
	}
	return execRunningCommand{process}, nil
}

type execRunningCommand struct{ command *exec.Cmd }

func (p execRunningCommand) Signal(signal os.Signal) error { return p.command.Process.Signal(signal) }
func (p execRunningCommand) Kill() error                   { return p.command.Process.Kill() }
func (p execRunningCommand) Wait() error                   { return p.command.Wait() }

// Input is the safe platform-neutral native input intent.
type Input struct {
	Kind string
	Key  string
	Text string
}

// Config customizes the native platform and command/environment seams.
type Config struct {
	OS        string
	Runner    CommandRunner
	LookupEnv func(string) string
}

// Actions owns only native display command selection and process mechanics.
type Actions struct {
	os        string
	runner    CommandRunner
	lookupEnv func(string) string
}

func New(config Config) Actions {
	if strings.TrimSpace(config.OS) == "" {
		config.OS = runtime.GOOS
	}
	if config.Runner == nil {
		config.Runner = ExecCommandRunner{}
	}
	if config.LookupEnv == nil {
		config.LookupEnv = os.Getenv
	}
	return Actions{os: strings.ToLower(strings.TrimSpace(config.OS)), runner: config.Runner, lookupEnv: config.LookupEnv}
}

func (a Actions) DesktopAvailable() error {
	switch a.os {
	case "darwin":
		if strings.TrimSpace(a.lookupEnv("CI")) != "" {
			return errors.New("macOS native display is unavailable in CI")
		}
	case "windows":
		if strings.EqualFold(strings.TrimSpace(a.lookupEnv("SESSIONNAME")), "Services") {
			return errors.New("Windows native display is unavailable in a service session")
		}
	}
	return nil
}

func (a Actions) Capture(ctx context.Context) ([]byte, error) {
	switch a.os {
	case "darwin":
		return a.runner.Run(ctx, "screencapture", "-x", "-t", "png", "-")
	case "windows":
		return a.runner.Run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", windowsScreenshotCommand)
	default:
		return a.runner.Run(ctx, "sh", "-c", "if command -v grim >/dev/null 2>&1; then grim -; elif command -v import >/dev/null 2>&1; then import -window root png:-; else exit 127; fi")
	}
}

func (a Actions) Input(ctx context.Context, input Input) error {
	if strings.TrimSpace(input.Kind) == "" {
		return errors.New("native display input kind is required")
	}
	switch a.os {
	case "darwin":
		if input.Kind != "key" && input.Kind != "text" {
			return fmt.Errorf("native macOS input kind %q is unsupported", input.Kind)
		}
		command := "keystroke " + appleScriptString(input.Text)
		if input.Kind == "key" {
			var err error
			command, err = keyCommands(input.Key, "darwin")
			if err != nil {
				return err
			}
		}
		_, err := a.runner.Run(ctx, "osascript", "-e", "tell application \"System Events\" to "+command)
		return err
	case "windows":
		if input.Kind != "key" && input.Kind != "text" {
			return fmt.Errorf("native Windows input kind %q is unsupported", input.Kind)
		}
		value := windowsLiteral(input.Text)
		if input.Kind == "key" {
			var err error
			value, err = keyCommands(input.Key, "windows")
			if err != nil {
				return err
			}
		}
		_, err := a.runner.Run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", "Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait("+powershellString(value)+")")
		return err
	default:
		if input.Kind != "key" {
			return fmt.Errorf("native Linux input kind %q is unsupported", input.Kind)
		}
		if strings.TrimSpace(a.lookupEnv("WAYLAND_DISPLAY")) != "" {
			_, err := a.runner.Run(ctx, "wtype", input.Key)
			return err
		}
		_, err := a.runner.Run(ctx, "xdotool", "key", input.Key)
		return err
	}
}

func (a Actions) RecordingArgs(path, format string) ([]string, error) {
	if strings.TrimSpace(path) == "" || (format != "mp4" && format != "webm") {
		return nil, errors.New("native display recording requires an output path and supported format")
	}
	codec := "libx264"
	if format == "webm" {
		codec = "libvpx-vp9"
	}
	args := []string{"-y", "-loglevel", "error"}
	switch a.os {
	case "darwin":
		args = append(args, "-f", "avfoundation", "-framerate", "30", "-i", "1:none")
	case "windows":
		args = append(args, "-f", "gdigrab", "-framerate", "30", "-i", "desktop")
	default:
		displayName := strings.TrimSpace(a.lookupEnv("DISPLAY"))
		if displayName == "" {
			return nil, errors.New("Linux native recording requires DISPLAY")
		}
		args = append(args, "-f", "x11grab", "-framerate", "30", "-i", displayName)
	}
	return append(args, "-c:v", codec, path), nil
}

func (a Actions) StartRecording(path, format string) (RunningCommand, error) {
	args, err := a.RecordingArgs(path, format)
	if err != nil {
		return nil, err
	}
	return a.runner.Start("ffmpeg", args...)
}

// StopRecording gracefully stops a native recording and kills it if the
// caller's deadline expires.
func StopRecording(ctx context.Context, process RunningCommand) error {
	if process == nil {
		return nil
	}
	_ = process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = process.Kill()
		return ctx.Err()
	}
}

func appleScriptString(value string) string {
	return "\"" + strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "\"", "\\\"") + "\""
}

func powershellString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
