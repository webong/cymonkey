// Package displayprobe contains the platform-specific display readiness
// mechanics for caller-selected host environments.
package displayprobe

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const x11SocketDirectory = "/tmp/.X11-unix"

const (
	CapabilityNative      = "runtime.display.native"
	CapabilityHostX11     = "runtime.display.host-x11"
	CapabilityHostWayland = "runtime.display.host-wayland"
	CapabilityContainer   = "runtime.display.container"
	CapabilityRecording   = "display.recording"
)

type Config struct {
	OS         string
	LookupEnv  func(string) (string, bool)
	Stat       func(string) error
	LookPath   func(string) (string, error)
	ReadFile   func(string) ([]byte, error)
	ObservedAt func() time.Time
}

type Probe struct{ config Config }

func New(config Config) Probe {
	if strings.TrimSpace(config.OS) == "" {
		config.OS = runtime.GOOS
	}
	if config.LookupEnv == nil {
		config.LookupEnv = os.LookupEnv
	}
	if config.Stat == nil {
		config.Stat = func(path string) error { _, err := os.Stat(path); return err }
	}
	if config.LookPath == nil {
		config.LookPath = exec.LookPath
	}
	if config.ReadFile == nil {
		config.ReadFile = os.ReadFile
	}
	if config.ObservedAt == nil {
		config.ObservedAt = func() time.Time { return time.Now().UTC() }
	}
	return Probe{config: config}
}

func (p Probe) Observe() Observation {
	observedAt := p.config.ObservedAt().UTC()
	value := Observation{
		Platform: p.config.OS, ObservedAt: observedAt,
		Containerized: p.containerized(), X11: p.x11(), Wayland: p.wayland(),
	}
	if p.config.OS == "linux" {
		value.Native = value.X11.Available || value.Wayland.Available
	} else {
		value.Native, value.Reason = p.nativeDesktop()
	}
	if value.X11.Available {
		value.Capabilities = append(value.Capabilities, CapabilityHostX11)
	}
	if value.Wayland.Available {
		value.Capabilities = append(value.Capabilities, CapabilityHostWayland)
	}
	if value.Native {
		value.Capabilities = append(value.Capabilities, CapabilityNative)
	}
	if value.Containerized {
		value.Capabilities = append(value.Capabilities, CapabilityContainer)
	}
	value.Recording = p.recording(observedAt)
	if value.Recording.Status == "ready" {
		value.Capabilities = append(value.Capabilities, CapabilityRecording)
	}
	if !value.Native && strings.TrimSpace(value.Reason) == "" {
		value.Reason = firstNonEmpty(value.X11.Reason, value.Wayland.Reason)
	}
	return value
}

func (p Probe) x11() Endpoint {
	displayName, ok := p.config.LookupEnv("DISPLAY")
	if !ok || strings.TrimSpace(displayName) == "" {
		return Endpoint{Reason: "DISPLAY environment variable is not set"}
	}
	socket, err := ResolveX11Socket(displayName)
	if err != nil {
		return Endpoint{Reason: "X11 display is unavailable"}
	}
	if err := p.config.Stat(socket); err != nil {
		return Endpoint{Reason: "X11 socket is unavailable"}
	}
	return Endpoint{Available: true}
}

func (p Probe) wayland() Endpoint {
	displayName, ok := p.config.LookupEnv("WAYLAND_DISPLAY")
	if !ok || strings.TrimSpace(displayName) == "" {
		return Endpoint{Reason: "WAYLAND_DISPLAY environment variable is not set"}
	}
	runtimeDir, ok := p.config.LookupEnv("XDG_RUNTIME_DIR")
	if !ok || strings.TrimSpace(runtimeDir) == "" {
		return Endpoint{Reason: "XDG_RUNTIME_DIR environment variable is not set"}
	}
	if err := p.config.Stat(filepath.Join(runtimeDir, displayName)); err != nil {
		return Endpoint{Reason: "Wayland socket is unavailable"}
	}
	return Endpoint{Available: true}
}

func (p Probe) nativeDesktop() (bool, string) {
	switch p.config.OS {
	case "darwin":
		if value, ok := p.config.LookupEnv("CI"); ok && strings.TrimSpace(value) != "" {
			return false, "macOS CI environment is not expected to expose an interactive desktop"
		}
		if _, ssh := p.config.LookupEnv("SSH_CONNECTION"); !ssh {
			if _, ssh = p.config.LookupEnv("SSH_CLIENT"); !ssh {
				if _, ssh = p.config.LookupEnv("SSH_TTY"); !ssh {
					return true, ""
				}
			}
		}
		if value, ok := p.config.LookupEnv("DISPLAY"); !ok || strings.TrimSpace(value) == "" {
			return false, "SSH session does not expose a local macOS display"
		}
		return true, ""
	case "windows":
		session, ok := p.config.LookupEnv("SESSIONNAME")
		if !ok || strings.TrimSpace(session) == "" {
			return false, "WINDOWS SESSIONNAME is not set"
		}
		if strings.EqualFold(strings.TrimSpace(session), "Services") {
			return false, "Windows session is running under service context"
		}
		return true, ""
	default:
		return false, fmt.Sprintf("host-native display probing is not available on this platform")
	}
}

func (p Probe) recording(now time.Time) RecordingCapability {
	value := RecordingCapability{CheckedAt: now, Permission: "not-required"}
	if _, err := p.config.LookPath("ffmpeg"); err != nil {
		value.Status, value.Message = "unsupported", "ffmpeg is not available"
		return value
	}
	value.EncoderAvailable = true
	value.Formats = []string{"mp4", "webm"}
	value.CaptureAvailable, value.Permission, value.Message = p.captureReadiness()
	if !value.CaptureAvailable {
		value.Status = "unsupported"
		if value.Message == "" {
			value.Message = "host display capture is unavailable"
		}
		return value
	}
	if value.Permission != "not-required" {
		value.Status = "capture_permission_required"
		return value
	}
	value.Status, value.Message = "ready", "display recording is ready"
	return value
}

func (p Probe) captureReadiness() (bool, string, string) {
	switch p.config.OS {
	case "darwin":
		available, reason := p.nativeDesktop()
		if !available {
			return false, "not-available", reason
		}
		return true, "macos.screen-recording", "grant Screen Recording permission to the calling application"
	case "windows":
		available, reason := p.nativeDesktop()
		return available, "not-required", reason
	case "linux":
		if x11 := p.x11(); x11.Available {
			return true, "not-required", ""
		}
		if wayland := p.wayland(); wayland.Available {
			return true, "linux.pipewire-portal", "approve screen capture through the desktop portal"
		}
		x11, wayland := p.x11(), p.wayland()
		return false, "not-available", strings.Trim(strings.Join([]string{x11.Reason, wayland.Reason}, "; "), "; ")
	default:
		return false, "not-available", fmt.Sprintf("display capture is unsupported on %s", p.config.OS)
	}
}

func (p Probe) containerized() bool {
	if value, ok := p.config.LookupEnv("container"); ok && strings.TrimSpace(value) != "" {
		return true
	}
	if p.config.Stat("/.dockerenv") == nil {
		return true
	}
	data, err := p.config.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	content := strings.ToLower(string(data))
	return strings.Contains(content, "/docker/") || strings.Contains(content, "/containerd/") || strings.Contains(content, "/podman/") || strings.Contains(content, "/kubepods/")
}

func ResolveX11Socket(value string) (string, error) {
	host, remainder, found := strings.Cut(strings.TrimSpace(value), ":")
	if !found {
		return "", fmt.Errorf("DISPLAY value has no display number")
	}
	if host != "" && host != "localhost" && host != "unix" {
		return "", fmt.Errorf("DISPLAY value points to a non-local display host")
	}
	if strings.Contains(remainder, ".") {
		remainder = strings.SplitN(remainder, ".", 2)[0]
	}
	if strings.TrimSpace(remainder) == "" {
		remainder = "0"
	}
	server, err := strconv.Atoi(remainder)
	if err != nil {
		return "", err
	}
	return filepath.Join(x11SocketDirectory, "X"+strconv.Itoa(server)), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
