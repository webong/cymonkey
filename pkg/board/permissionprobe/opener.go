// Package permissionprobe contains bounded operating-system permission
// onboarding mechanics for callers that have authorized opening settings.
package permissionprobe

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// CommandRunner is the native command seam used to open an operating-system
// settings surface. It deliberately returns no command output to callers.
type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

// ExecCommandRunner opens a native settings command and captures its output
// locally. The output is intentionally discarded by Opener.
type ExecCommandRunner struct{}

func (ExecCommandRunner) Run(ctx context.Context, command string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, command, args...).Output()
}

// Config customizes the platform and command runner for an Opener.
type Config struct {
	OS     string
	Runner CommandRunner
}

// Result is the safe result of a permission onboarding request.
type Result struct {
	Permission string
	Opened     bool
	Message    string
}

// Opener owns only the platform mapping and command invocation. Authorization
// and whether onboarding may occur remain responsibilities of the caller.
type Opener struct {
	os     string
	runner CommandRunner
}

func New(config Config) Opener {
	if strings.TrimSpace(config.OS) == "" {
		config.OS = runtime.GOOS
	}
	if config.Runner == nil {
		config.Runner = ExecCommandRunner{}
	}
	return Opener{os: strings.ToLower(strings.TrimSpace(config.OS)), runner: config.Runner}
}

// Open launches the operating-system settings flow for a supported display
// permission. Linux X11 is already available and therefore needs no command.
func (o Opener) Open(ctx context.Context, permission string) (Result, error) {
	permission = strings.TrimSpace(permission)
	if permission == "" {
		return Result{}, fmt.Errorf("permission is required")
	}
	var command string
	var args []string
	switch o.os {
	case "darwin":
		if permission != "macos.screen-recording" {
			return Result{}, fmt.Errorf("unsupported macOS permission %s", permission)
		}
		command, args = "open", []string{"x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"}
	case "windows":
		if permission != "windows.screen-capture" {
			return Result{}, fmt.Errorf("unsupported Windows permission %s", permission)
		}
		command, args = "cmd", []string{"/c", "start", "ms-settings:privacy"}
	case "linux":
		switch permission {
		case "linux.pipewire-portal":
			command, args = "xdg-open", []string{"settings://privacy"}
		case "linux.x11":
			return Result{Permission: permission, Message: "X11 does not require a separate settings flow"}, nil
		default:
			return Result{}, fmt.Errorf("unsupported Linux permission %s", permission)
		}
	default:
		return Result{}, fmt.Errorf("permission onboarding is unsupported on %s", o.os)
	}
	if _, err := o.runner.Run(ctx, command, args...); err != nil {
		return Result{}, errors.New("open permission settings failed")
	}
	return Result{Permission: permission, Opened: true, Message: "follow the operating system prompt"}, nil
}
