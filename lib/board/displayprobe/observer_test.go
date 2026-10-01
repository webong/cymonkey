package displayprobe

import "testing"

func TestResolveX11Socket(t *testing.T) {
	tests := []struct {
		name, display, want string
		wantError           bool
	}{
		{name: "local default", display: ":0", want: "/tmp/.X11-unix/X0"},
		{name: "local with screen", display: ":0.0", want: "/tmp/.X11-unix/X0"},
		{name: "local alias", display: "localhost:2.0", want: "/tmp/.X11-unix/X2"},
		{name: "unix alias", display: "unix:3", want: "/tmp/.X11-unix/X3"},
		{name: "invalid", display: "otherhost:0", wantError: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			value, err := ResolveX11Socket(testCase.display)
			if testCase.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil || value != testCase.want {
				t.Fatalf("socket=%q err=%v", value, err)
			}
		})
	}
}

func TestObserveWindowsInteractiveDisplay(t *testing.T) {
	probe := New(Config{
		OS: "windows",
		LookupEnv: func(key string) (string, bool) {
			if key == "SESSIONNAME" {
				return "Console", true
			}
			return "", false
		},
		Stat:     func(string) error { return errMissing{} },
		LookPath: func(string) (string, error) { return "ffmpeg", nil },
	})
	value := probe.Observe()
	if !value.Native || value.Recording.Status != "ready" || value.Platform != "windows" {
		t.Fatalf("unexpected display observation: %+v", value)
	}
}

func TestObserveDoesNotLeakDisplayPaths(t *testing.T) {
	probe := New(Config{
		OS: "linux",
		LookupEnv: func(key string) (string, bool) {
			switch key {
			case "DISPLAY":
				return ":42", true
			case "WAYLAND_DISPLAY":
				return "xallet-wayland-0", true
			case "XDG_RUNTIME_DIR":
				return "/private/runtime", true
			}
			return "", false
		},
		Stat: func(path string) error {
			if path == "/tmp/.X11-unix/X42" || path == "/private/runtime/xallet-wayland-0" {
				return nil
			}
			return errMissing{}
		},
		ReadFile: func(string) ([]byte, error) { return nil, errMissing{} },
		LookPath: func(string) (string, error) { return "", errMissing{} },
	})
	value := probe.Observe()
	if !value.Native || !value.X11.Available || !value.Wayland.Available {
		t.Fatalf("unexpected display observation: %+v", value)
	}
	if value.X11.Reason != "" || value.Wayland.Reason != "" {
		t.Fatalf("unexpected endpoint details: %+v", value)
	}
}

type errMissing struct{}

func (errMissing) Error() string { return "missing" }
