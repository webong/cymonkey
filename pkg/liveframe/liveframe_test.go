package liveframe

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureStatus(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := json.Marshal(canvasStatus{
		Running: true, BrowserAlive: true, PID: 31, Dir: dir,
		HTTPAddr: "127.0.0.1", HTTPPort: 4011,
		CurrentURL: "http://127.0.0.1:4011/",
		BrowserPID: 32, DevtoolsPort: 4012,
		DevtoolsWSURL: "ws://127.0.0.1:4012/devtools/browser/fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseStatusAndTarget(t *testing.T) {
	t.Parallel()
	session, err := parseStatus(fixtureStatus(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if session.Generation == "" || session.BrowserPID != 32 {
		t.Fatalf("unexpected session: %+v", session)
	}
	target := session.Target()
	if target.Kind != "browser" || target.TargetID != "liveframe-canvas-31" {
		t.Fatalf("unexpected target: %+v", target)
	}
	endpoint, ok := target.Endpoint("cdp")
	if !ok || endpoint.URL != session.CDPURL {
		t.Fatalf("unexpected endpoint: %+v", endpoint)
	}
	spec := session.PresentationSpec()
	if spec.Adapter != "web-presentation" || spec.Source != session.PageURL {
		t.Fatalf("unexpected presentation spec: %+v", spec)
	}
	if !strings.Contains(string(spec.Options), `"allowedSourceOrigins":["http://127.0.0.1:4011"]`) {
		t.Fatalf("source origin not bounded: %s", spec.Options)
	}
	request, err := session.CymonkeyRequest("liveframe-fixture")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"apiVersion":"cymonkey.display/v1alpha1"`, `"targetId":"liveframe-canvas-31"`, `"requiredActions":["presentation.write","presentation.execute","presentation.capture"]`} {
		if !strings.Contains(string(wire), field) {
			t.Fatalf("connect request missing %s: %s", field, wire)
		}
	}
}

func TestParseStatusRejectsUnavailableAndNonlocalEndpoints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := parseStatus([]byte(`{"running":false,"browser_alive":false}`)); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("stopped status error = %v", err)
	}
	for name, change := range map[string]func(*canvasStatus){
		"external page": func(s *canvasStatus) { s.CurrentURL = "https://example.org/" },
		"external CDP":  func(s *canvasStatus) { s.DevtoolsWSURL = "ws://example.org:4012/devtools/browser/fixture" },
		"wrong port":    func(s *canvasStatus) { s.DevtoolsPort = 4013 },
		"no workspace":  func(s *canvasStatus) { s.Dir = "relative" },
	} {
		t.Run(name, func(t *testing.T) {
			var status canvasStatus
			if err := json.Unmarshal(fixtureStatus(t, dir), &status); err != nil {
				t.Fatal(err)
			}
			change(&status)
			data, _ := json.Marshal(status)
			if _, err := parseStatus(data); err == nil {
				t.Fatal("invalid Canvas status was accepted")
			}
		})
	}
}

func TestDiscoverAndRefresh(t *testing.T) {
	workspace := t.TempDir()
	stateDir := t.TempDir()
	statusPath := filepath.Join(t.TempDir(), "status.json")
	canvasPath := filepath.Join(t.TempDir(), "canvas")
	writeStatus := func(browserPID int) {
		t.Helper()
		var status canvasStatus
		if err := json.Unmarshal(fixtureStatus(t, workspace), &status); err != nil {
			t.Fatal(err)
		}
		status.BrowserPID = browserPID
		data, _ := json.Marshal(status)
		if err := os.WriteFile(statusPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeStatus(32)
	script := "#!/bin/sh\n" +
		"test \"$1\" = status && test \"$2\" = --json || exit 2\n" +
		"test \"$CANVAS_STATE_DIR\" = '" + stateDir + "' || exit 3\n" +
		"exec /bin/cat '" + statusPath + "'\n"
	if err := os.WriteFile(canvasPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	connector := Connector{CanvasPath: canvasPath, StateDir: stateDir}
	first, err := connector.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	current, changed, err := connector.Refresh(context.Background(), first)
	if err != nil || changed || current.Generation != first.Generation {
		t.Fatalf("unchanged refresh = %+v, %v, %v", current, changed, err)
	}
	writeStatus(33)
	current, changed, err = connector.Refresh(context.Background(), first)
	if err != nil || !changed || current.BrowserPID != 33 {
		t.Fatalf("restarted refresh = %+v, %v, %v", current, changed, err)
	}
}

func TestInstallStarterDoesNotOverwrite(t *testing.T) {
	t.Parallel()
	workspace := filepath.Join(t.TempDir(), "workspace")
	path, err := InstallStarter(workspace)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "window.cymonkey") {
		t.Fatalf("starter bridge missing: %v", err)
	}
	if _, err := InstallStarter(workspace); err == nil {
		t.Fatal("starter installer overwrote existing index.html")
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(data) {
		t.Fatal("starter page changed after rejected reinstall")
	}
}
