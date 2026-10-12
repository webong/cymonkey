package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"cymonkey/src/internal/browsermanage"
	"github.com/webong/ext/res/web/extension"
)

// browserTarget is a discovered browser profile and the adapter that reported it.
type browserTarget struct {
	extension.BrowserTarget
	Adapter string `json:"adapter"`
}

// discoverBrowserTargets asks every trusted adapter that declares
// extension.targets for its installations and profiles. Adapters that are not
// trusted are skipped; trust is the user's decision, made with cymy adapters.
func discoverBrowserTargets(ctx context.Context) ([]browserTarget, error) {
	host := browserHost()
	adapters, err := host.Adapters("extension.targets")
	if err != nil {
		return nil, err
	}
	targets := make([]browserTarget, 0)
	var failures []error
	for _, candidate := range adapters {
		trusted, err := host.Store().IsTrusted(candidate)
		if err != nil || !trusted {
			continue
		}
		response, err := host.Do(ctx, browsermanage.Endpoint{Adapter: candidate}, "extension", "targets", nil, nil)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", candidate.Manifest.Name, err))
			continue
		}
		var found []extension.BrowserTarget
		if err := browsermanage.Result(response, &found); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", candidate.Manifest.Name, err))
			continue
		}
		for _, target := range found {
			targets = append(targets, browserTarget{BrowserTarget: target, Adapter: candidate.Manifest.Name})
		}
	}
	if len(targets) == 0 && len(failures) > 0 {
		return nil, errors.Join(failures...)
	}
	return targets, nil
}

func findBrowserTarget(ctx context.Context, id string) (browserTarget, error) {
	targets, err := discoverBrowserTargets(ctx)
	if err != nil {
		return browserTarget{}, err
	}
	for _, target := range targets {
		if target.ID == id {
			return target, nil
		}
	}
	return browserTarget{}, fmt.Errorf("browser target %q was not found; list targets with cymy browser targets", id)
}

// browserSelector is how a caller names a browser: a discovered target ID, or
// an adapter name with optional explicit installation and profile paths.
type browserSelector struct {
	ID               string
	Browser          string
	Executable       string
	ProfilePath      string
	ProfileDirectory string
}

func (selector browserSelector) explicit() bool {
	return selector.Browser != "" || selector.Executable != "" || selector.ProfilePath != "" || selector.ProfileDirectory != ""
}

// endpoint resolves the selector to an adapter endpoint plus the input fields
// that bind the profile for that adapter.
func (selector browserSelector) endpoint(ctx context.Context) (browsermanage.Endpoint, map[string]any, error) {
	host := browserHost()
	fields := map[string]any{}
	if selector.ID != "" {
		if selector.explicit() {
			return browsermanage.Endpoint{}, nil, errors.New("a target ID cannot be combined with a browser, executable, or profile")
		}
		target, err := findBrowserTarget(ctx, selector.ID)
		if err != nil {
			return browsermanage.Endpoint{}, nil, err
		}
		endpoint, err := host.Endpoint(target.Adapter + ":" + target.Profile)
		fields["targetId"] = target.ID
		return endpoint, fields, err
	}
	if selector.Browser == "" {
		return browsermanage.Endpoint{}, nil, errors.New("select a target ID or an adapter with --browser")
	}
	profile := selector.ProfilePath
	if profile != "" && !filepath.IsAbs(profile) {
		return browsermanage.Endpoint{}, nil, errors.New("profile must be an absolute path")
	}
	if profile == "" {
		profile = selector.ProfileDirectory
	}
	endpoint, err := host.Endpoint(selector.Browser + ":" + profile)
	if selector.Executable != "" {
		fields["executable"] = selector.Executable
	}
	if selector.ProfilePath != "" {
		fields["profilePath"] = selector.ProfilePath
	}
	if selector.ProfileDirectory != "" {
		fields["profileDirectory"] = selector.ProfileDirectory
	}
	return endpoint, fields, err
}

// adapterEndpoint selects an adapter for an operation that needs no profile.
// An explicit browser name wins; otherwise exactly one trusted adapter must
// declare the operation.
func adapterEndpoint(browser, operation string) (browsermanage.Endpoint, error) {
	host := browserHost()
	if strings.TrimSpace(browser) != "" {
		return host.Endpoint(browser)
	}
	adapters, err := host.Adapters(operation)
	if err != nil {
		return browsermanage.Endpoint{}, err
	}
	var trusted []string
	for _, candidate := range adapters {
		if ok, err := host.Store().IsTrusted(candidate); err == nil && ok {
			trusted = append(trusted, candidate.Manifest.Name)
		}
	}
	switch len(trusted) {
	case 0:
		return browsermanage.Endpoint{}, fmt.Errorf("no trusted adapter declares %s; install one with cymy adapters install and trust it", operation)
	case 1:
		return host.Endpoint(trusted[0])
	default:
		return browsermanage.Endpoint{}, fmt.Errorf("several adapters declare %s (%s); select one with --browser", operation, strings.Join(trusted, ", "))
	}
}

// manageExtension runs one extension action. progress receives each progress
// value an adapter reports. It returns the adapter's response.
func manageExtension(ctx context.Context, endpoint browsermanage.Endpoint, action string, input map[string]any, progress browsermanage.Progress) (json.RawMessage, error) {
	response, err := browserHost().Do(ctx, endpoint, "extension", action, input, progress)
	if err != nil {
		return nil, err
	}
	if len(response.Result) > 0 {
		return response.Result, nil
	}
	return json.Marshal(map[string]string{"status": response.Status})
}
