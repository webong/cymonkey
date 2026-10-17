// Package liveframe connects a caller-owned Canvas workspace to Cymonkey's
// web-presentation adapter. Canvas owns the server and browser lifecycle;
// Liveframe only discovers and describes the running session.
package liveframe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"board/display/sdk"
)

const defaultCommandTimeout = 5 * time.Second

const targetAPIVersion = "cymonkey.display-target/v1alpha1"

// ErrNotRunning means Canvas has no live browser in the selected state dir.
var ErrNotRunning = errors.New("Canvas is not running with a live browser")

// Connector selects an installed Canvas CLI and, optionally, a caller-owned
// Canvas state directory. It never invokes Canvas start, stop, goto, or reload.
type Connector struct {
	CanvasPath string
	StateDir   string
	Timeout    time.Duration
}

// Session is one observed Canvas workspace. A new browser or server process
// changes Generation and requires the caller to detach and attach again.
type Session struct {
	WorkspaceDir string
	PageURL      string
	CDPURL       string
	ServerPID    int
	BrowserPID   int
	Generation   string
}

type canvasStatus struct {
	Running       bool   `json:"running"`
	BrowserAlive  bool   `json:"browser_alive"`
	PID           int    `json:"pid"`
	Dir           string `json:"dir"`
	HTTPAddr      string `json:"http_addr"`
	HTTPPort      int    `json:"http_port"`
	CurrentURL    string `json:"current_url"`
	BrowserPID    int    `json:"browser_pid"`
	DevtoolsPort  int    `json:"devtools_port"`
	DevtoolsWSURL string `json:"devtools_ws_url"`
}

// Discover reads Canvas status without changing its browser, page, or files.
func (c Connector) Discover(ctx context.Context) (Session, error) {
	command := strings.TrimSpace(c.CanvasPath)
	if command == "" {
		command = "canvas"
	}
	resolved, err := exec.LookPath(command)
	if err != nil {
		return Session{}, fmt.Errorf("find Canvas CLI: %w", err)
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(callCtx, resolved, "status", "--json")
	if c.StateDir != "" {
		if !filepath.IsAbs(c.StateDir) {
			return Session{}, errors.New("Canvas state directory must be absolute")
		}
		cmd.Env = append(os.Environ(), "CANVAS_STATE_DIR="+c.StateDir)
	}
	output, err := cmd.Output()
	if err != nil {
		if callCtx.Err() != nil {
			return Session{}, fmt.Errorf("Canvas status: %w", callCtx.Err())
		}
		return Session{}, fmt.Errorf("Canvas status: %w", err)
	}
	session, err := parseStatus(output)
	if err != nil {
		return Session{}, err
	}
	info, err := os.Stat(session.WorkspaceDir)
	if err != nil || !info.IsDir() {
		return Session{}, fmt.Errorf("Canvas workspace directory is unavailable: %s", session.WorkspaceDir)
	}
	return session, nil
}

// Refresh discovers the current session. changed is true when a caller should
// detach its old web-presentation instance and attach to the returned target.
// An ordinary page reload retains the same session and needs no reattachment.
func (c Connector) Refresh(ctx context.Context, previous Session) (current Session, changed bool, err error) {
	current, err = c.Discover(ctx)
	if err != nil {
		return Session{}, false, err
	}
	return current, current.Generation != previous.Generation || current.PageURL != previous.PageURL || current.WorkspaceDir != previous.WorkspaceDir, nil
}

func parseStatus(data []byte) (Session, error) {
	var status canvasStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return Session{}, fmt.Errorf("decode Canvas status: %w", err)
	}
	if !status.Running || !status.BrowserAlive {
		return Session{}, ErrNotRunning
	}
	if status.PID <= 0 || status.BrowserPID <= 0 || !filepath.IsAbs(status.Dir) {
		return Session{}, errors.New("Canvas status has invalid process or workspace coordinates")
	}
	if !localAddress(status.HTTPAddr) || !validPort(status.HTTPPort) || !validPort(status.DevtoolsPort) {
		return Session{}, errors.New("Canvas status must expose local HTTP and CDP ports")
	}
	page, err := url.Parse(status.CurrentURL)
	if err != nil || page.Scheme != "http" || page.User != nil || !localAddress(page.Hostname()) || page.Port() != strconv.Itoa(status.HTTPPort) {
		return Session{}, errors.New("Canvas current URL is not on its local workspace server")
	}
	cdp, err := url.Parse(status.DevtoolsWSURL)
	if err != nil || cdp.Scheme != "ws" || cdp.User != nil || !localAddress(cdp.Hostname()) || cdp.Port() != strconv.Itoa(status.DevtoolsPort) || !strings.HasPrefix(cdp.Path, "/devtools/browser/") {
		return Session{}, errors.New("Canvas CDP URL is not a local browser endpoint")
	}
	return Session{
		WorkspaceDir: status.Dir,
		PageURL:      page.String(),
		CDPURL:       cdp.String(),
		ServerPID:    status.PID,
		BrowserPID:   status.BrowserPID,
		Generation:   fmt.Sprintf("%d:%d:%s", status.PID, status.BrowserPID, cdp.String()),
	}, nil
}

func localAddress(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func validPort(port int) bool { return port > 0 && port <= 65535 }

// Target returns the caller-owned browser coordinates for web-presentation.
func (s Session) Target() sdk.EngineTarget {
	return sdk.EngineTarget{
		APIVersion: targetAPIVersion,
		TargetID:   fmt.Sprintf("liveframe-canvas-%d", s.ServerPID),
		Kind:       "browser",
		Endpoints:  []sdk.TargetEndpoint{{Name: "canvas-cdp", Protocol: "cdp", URL: s.CDPURL}},
		Metadata:   map[string]string{"provider": "canvas"},
	}
}

// PresentationSpec selects Cymonkey's existing semantic presentation adapter.
// The source and asset policy are limited to the Canvas workspace origin.
func (s Session) PresentationSpec() sdk.EngineSpec {
	page, _ := url.Parse(s.PageURL)
	origin := page.Scheme + "://" + page.Host
	options, _ := json.Marshal(map[string]any{"policy": map[string]any{
		"allowedSourceOrigins": []string{origin},
		"allowedAssetOrigins":  []string{"self", "data:", "blob:"},
		"authorizedActions":    []string{"presentation.capture", "presentation.execute"},
	}})
	return sdk.EngineSpec{
		Adapter:              "web-presentation",
		RequiredCapabilities: []string{"presentation.write", "presentation.capture"},
		Source:               s.PageURL,
		Options:              options,
	}
}

// ConnectRequest is the JSON shape accepted by Cymonkey's display service.
// The request deliberately contains only coordinates and a policy; it does
// not grant approval or launch the caller-owned Canvas browser.
type ConnectRequest struct {
	APIVersion string        `json:"apiVersion"`
	InstanceID string        `json:"instanceId"`
	Engine     ConnectEngine `json:"engine"`
	Target     ConnectTarget `json:"target"`
}

type ConnectEngine struct {
	sdk.EngineSpec
	Approval ConnectApproval `json:"approval"`
}

type ConnectApproval struct {
	RequiredActions []string `json:"requiredActions"`
}

type ConnectTarget struct {
	APIVersion string            `json:"apiVersion"`
	TargetID   string            `json:"targetId"`
	Kind       string            `json:"kind"`
	Endpoints  []ConnectEndpoint `json:"endpoints"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type ConnectEndpoint struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	URL      string `json:"url"`
}

// CymonkeyRequest builds an approved-action-aware service request. The host
// still decides whether to issue an approval for each requested action.
func (s Session) CymonkeyRequest(instanceID string) (ConnectRequest, error) {
	if instanceID == "" {
		return ConnectRequest{}, errors.New("Liveframe instance ID is required")
	}
	request := ConnectRequest{
		APIVersion: "cymonkey.display/v1alpha1",
		InstanceID: instanceID,
		Engine:     ConnectEngine{EngineSpec: s.PresentationSpec()},
		Target: ConnectTarget{
			APIVersion: targetAPIVersion,
			TargetID:   s.Target().TargetID,
			Kind:       "browser",
			Metadata:   map[string]string{"provider": "canvas"},
		},
	}
	request.Engine.Approval.RequiredActions = []string{"presentation.write", "presentation.execute", "presentation.capture"}
	request.Target.Endpoints = []ConnectEndpoint{{Name: "canvas-cdp", Protocol: "cdp", URL: s.CDPURL}}
	return request, nil
}
