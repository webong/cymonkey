package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"cymonkey/src/internal/engineprovider"
	"jangolova/browserextension"
)

// browserSessionRequest is the Cymonkey-specific shortcut for the generic
// interaction-engine connection contract. It never launches a browser.
type browserSessionRequest struct {
	InstanceID           string                        `json:"instanceId,omitempty"`
	TargetID             string                        `json:"targetId,omitempty"`
	Browser              string                        `json:"browser,omitempty"`
	BrowserBin           string                        `json:"browserBin,omitempty"`
	Profile              string                        `json:"profile,omitempty"`
	ProfileDirectory     string                        `json:"profileDirectory,omitempty"`
	Endpoint             string                        `json:"endpoint,omitempty"`
	Adapter              string                        `json:"adapter,omitempty"`
	RequiredCapabilities []string                      `json:"requiredCapabilities,omitempty"`
	Options              json.RawMessage               `json:"options,omitempty"`
	Approval             engineprovider.ApprovalPolicy `json:"approval,omitempty"`
	UserscriptsTarget    string                        `json:"userscriptsTarget,omitempty"`
}

func (request browserSessionRequest) connectRequest() (engineprovider.ConnectRequest, string, error) {
	selected, err := request.selectedTarget()
	if err != nil {
		return engineprovider.ConnectRequest{}, "", err
	}
	protocol, address, err := splitBrowserEndpoint(request.Endpoint)
	if err != nil {
		return engineprovider.ConnectRequest{}, "", err
	}
	if address == "" {
		if selected.Browser == "" {
			return engineprovider.ConnectRequest{}, "", errors.New("select a browser target or provide an endpoint")
		}
		protocol, address, err = browserextension.ActiveEndpoint(selected)
		if err != nil {
			return engineprovider.ConnectRequest{}, "", err
		}
	}
	instanceID := strings.TrimSpace(request.InstanceID)
	if instanceID == "" {
		var random [6]byte
		if _, err := rand.Read(random[:]); err != nil {
			return engineprovider.ConnectRequest{}, "", err
		}
		instanceID = "browser-" + hex.EncodeToString(random[:])
	}
	adapter := strings.TrimSpace(request.Adapter)
	if adapter == "" {
		adapter = "auto"
	}
	options := request.Options
	if len(options) == 0 {
		options = []byte("{}")
	}
	userscriptsTarget := strings.TrimSpace(request.UserscriptsTarget)
	if userscriptsTarget == "" {
		userscriptsTarget = selected.ID
	}
	if userscriptsTarget != "" && selected.ID != "" && userscriptsTarget != selected.ID {
		return engineprovider.ConnectRequest{}, "", errors.New("userscriptsTarget must match the selected browser target")
	}
	capabilities := append([]string(nil), request.RequiredCapabilities...)
	if request.UserscriptsTarget != "" {
		capabilities = append(capabilities, "script.register")
	}
	target := engineprovider.Target{Kind: "browser", Endpoints: []engineprovider.TargetEndpoint{{Name: protocol, Protocol: protocol, URL: address}}}
	if selected.ID != "" {
		target.APIVersion = engineprovider.TargetAPIVersion
		target.TargetID = selected.ID
	}
	return engineprovider.ConnectRequest{
		APIVersion: engineprovider.APIVersion,
		InstanceID: instanceID,
		Engine:     engineprovider.EngineSpec{Adapter: adapter, RequiredCapabilities: capabilities, Options: options, Approval: request.Approval},
		Target:     target,
	}, userscriptsTarget, nil
}

func (request browserSessionRequest) selectedTarget() (browserextension.BrowserTarget, error) {
	if request.TargetID != "" {
		if request.Browser != "" || request.BrowserBin != "" || request.Profile != "" || request.ProfileDirectory != "" {
			return browserextension.BrowserTarget{}, errors.New("targetId cannot be combined with explicit browser or profile paths")
		}
		return browserextension.ResolveTarget(request.TargetID)
	}
	if request.Browser != "" || request.BrowserBin != "" || request.Profile != "" || request.ProfileDirectory != "" {
		if request.Browser == "" {
			return browserextension.BrowserTarget{}, errors.New("browser is required with explicit browser paths")
		}
		if request.Profile != "" && !filepath.IsAbs(request.Profile) {
			return browserextension.BrowserTarget{}, errors.New("profile must be an absolute path")
		}
		return browserextension.BrowserTarget{Browser: request.Browser, ExecutablePath: request.BrowserBin, ProfilePath: request.Profile, ProfileDirectory: request.ProfileDirectory}, nil
	}
	if request.Endpoint != "" {
		return browserextension.BrowserTarget{}, nil
	}
	targets, err := browserextension.DiscoverTargets()
	if err != nil {
		return browserextension.BrowserTarget{}, err
	}
	var active []browserextension.BrowserTarget
	for _, target := range targets {
		if _, _, err := browserextension.ActiveEndpoint(target); err == nil {
			active = append(active, target)
		}
	}
	if len(active) == 1 {
		return active[0], nil
	}
	if len(active) > 1 {
		return browserextension.BrowserTarget{}, errors.New("multiple browser profiles have active debugging endpoints; select targetId")
	}
	return browserextension.BrowserTarget{}, errors.New("no active browser debugging endpoint was found; select targetId and supply endpoint")
}

func splitBrowserEndpoint(value string) (string, string, error) {
	if strings.TrimSpace(value) == "" {
		return "", "", nil
	}
	protocol, address, ok := strings.Cut(value, "=")
	if !ok || strings.TrimSpace(protocol) == "" || strings.TrimSpace(address) == "" {
		return "", "", errors.New("endpoint must use PROTOCOL=URL")
	}
	switch protocol {
	case "cdp", "webdriver-bidi", "webdriver", "mcp-streamable-http":
		return protocol, address, nil
	default:
		return "", "", fmt.Errorf("unsupported browser endpoint protocol %q", protocol)
	}
}
