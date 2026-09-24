package boundary_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRepositoryOwnsInteractionCodeOnly prevents target-runtime provisioning
// and combined-session responsibilities from returning to Jangolova.
func TestRepositoryOwnsEngineCodeOnly(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve boundary test location")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
	productRoots := []string{"src/adapters", "src/fixtures", "src/internal"}
	for _, productRoot := range productRoots {
		path := filepath.Join(root, productRoot)
		if err := filepath.WalkDir(path, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if relative != "src/internal/boundary" && forbiddenPathComponent(entry.Name()) {
					t.Errorf("display-runtime directory is outside tests: %s", relative)
				}
				return nil
			}
			if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, fragment := range forbiddenSourceFragments {
				if strings.Contains(string(contents), fragment) {
					t.Errorf("display-runtime API %q is outside tests: %s", fragment, relative)
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("scan %s: %v", productRoot, err)
		}
	}

	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative == ".git" || relative == "tests" || relative == ".cache" || entry.Name() == "node_modules" || entry.Name() == ".output" || entry.Name() == ".wxt" {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(entry.Name())
		if (name == "dockerfile" || name == "containerfile" || strings.Contains(name, "compose")) &&
			!allowedDeploymentFixture(relative) {
			t.Errorf("deployment topology must live under tests or Xallet: %s", relative)
		}
		if relative == "infra/deploy/engine-runtime/Containerfile" {
			contents, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			lower := strings.ToLower(string(contents))
			for _, fragment := range []string{"chromium", "google-chrome", "firefox", "xvfb", "webkit"} {
				if strings.Contains(lower, fragment) {
					t.Errorf("interaction runtime image packages target runtime %q", fragment)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("scan deployment files: %v", err)
	}
}

func allowedDeploymentFixture(relative string) bool {
	switch relative {
	case "infra/deploy/engine-runtime/Containerfile",
		"infra/deploy/blender-cymonkey-fixture/Containerfile",
		"infra/deploy/godot-cymonkey-fixture/Containerfile",
		"infra/deploy/godot-cymonkey-gpu/Containerfile",
		"infra/deploy/unity-cymonkey-fixture/Containerfile",
		"infra/deploy/unity-cymonkey-gpu/Containerfile",
		"infra/deploy/unreal-cymonkey-fixture/Containerfile",
		"infra/deploy/unreal-cymonkey-gpu/Containerfile",
		"infra/deploy/blockade/Containerfile":
		return true
	default:
		return false
	}
}

var forbiddenSourceFragments = []string{
	"type Surface interface",
	"type SurfaceAdapter interface",
	"type ControllerAdapter interface",
	"type ConnectorAdapter interface",
	"type Session struct",
	"RegisterSurface(",
	"RegisterController(",
	"RegisterConnector(",
	"NewSession(",
	"RegisterEngine(\"chromium\"",
	"RegisterEngine(\"native-process\"",
	"--remote-debugging-port=",
	"LaunchPersistentContext(",
	"puppeteer.launch(",
	"exec.Command(\"safaridriver\"",
	"exec.Command(\"firefox",
	"exec.Command(\"WebKitWebDriver\"",
	"exec.Command(\"WPEWebDriver\"",
}

func forbiddenPathComponent(name string) bool {
	switch strings.ToLower(name) {
	case "controller", "controllers", "connector", "connectors",
		"placement", "session", "sessions", "surface", "surfaces", "vnc",
		"webrtc", "xallet", "xpost", "xvfb":
		return true
	default:
		return false
	}
}
