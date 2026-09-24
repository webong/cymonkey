package jangolova

import (
	"strings"

	"context"
	"errors"
	"fmt"

	contract "jangolova/contract"
	"jangolova/sdk"
)

type processBackend struct {
	name             BackendName
	endpointProtocol string
}

func (backend processBackend) Name() BackendName { return backend.name }
func (backend processBackend) Domains() []contract.Domain {
	return []contract.Domain{contract.DomainViewer, contract.DomainRender}
}
func (backend processBackend) Compatible(target sdk.EngineTarget) bool {
	_, ok := target.Endpoint(backend.endpointProtocol)
	return target.Kind == "browser" && ok
}

var configuredBackends = []Backend{
	processBackend{name: BackendCDP, endpointProtocol: "cdp"},
	processBackend{name: BackendBiDi, endpointProtocol: "webdriver-bidi"},
	safariMCPBackend{},
	macOSCooperativeBackend{},
	windowsCooperativeBackend{},
	enginePresentationBackend{},
}

func (a Adapter) Connect(ctx context.Context, spec sdk.EngineSpec, target sdk.EngineTarget) (sdk.EngineInstance, error) {
	config, err := decodeOptions(spec.Options)
	if err != nil {
		return nil, err
	}
	config.Host = a.Host
	if config.Composite != nil {
		return nil, errors.New("Jangolova composite attachment migration is pending; use Cymonkey core composition directly")
	}
	// Explicitly registered external modules select their own target kinds.
	for _, backend := range a.Backends {
		if backend.Compatible(target) && (config.Driver == "auto" || backendMatchesRequest(backend, config.Driver)) {
			if config.Domain != "" && !containsDomain(backend.Domains(), config.Domain) {
				continue
			}
			return backend.Connect(ctx, spec, target, config)
		}
	}
	domain, err := resolveDomain(config.Domain, target)
	if err != nil {
		return nil, err
	}
	backend, err := selectBackendForDomain(config.Driver, domain, target)
	if err != nil {
		return nil, err
	}
	return backend.Connect(ctx, spec, target, config)
}

func selectBackend(requested string, target sdk.EngineTarget) (Backend, error) {
	return selectBackendForDomain(requested, contract.DomainViewer, target)
}

func selectBackendForDomain(requested string, domain contract.Domain, target sdk.EngineTarget) (Backend, error) {
	for _, backend := range configuredBackends {
		if !containsDomain(backend.Domains(), domain) {
			continue
		}
		if requested != "auto" && !backendMatchesRequest(backend, requested) {
			continue
		}
		if backend.Compatible(target) {
			return backend, nil
		}
	}
	if requested == "auto" {
		if domain == contract.DomainViewer && target.Kind == "macos-application" {
			return nil, errors.New("Cymonkey viewer domain for macOS requires a caller-owned native helper; Apple Events and Accessibility are not invoked directly by the provider")
		}
		if domain == contract.DomainViewer && target.Kind == "windows-application" {
			return nil, errors.New("Cymonkey viewer domain for Windows requires a caller-owned native helper; Win32 input is not invoked directly by the provider")
		}
		if domain == contract.DomainRender {
			return nil, errors.New("Cymonkey render domain requires a caller-owned websocket presentation endpoint")
		}
		if domain == contract.DomainPlayer {
			return nil, errors.New("Cymonkey player domain has no negotiated driver for this target")
		}
		return nil, errors.New("Cymonkey viewer domain requires a caller-owned CDP, WebDriver BiDi, or Safari MCP endpoint")
	}
	return nil, fmt.Errorf("Cymonkey backend %s has no compatible caller-owned %s domain target", requested, domain)
}

func resolveDomain(requested contract.Domain, target sdk.EngineTarget) (contract.Domain, error) {
	if requested == "" {
		switch target.Kind {
		case "browser":
			return contract.DomainViewer, nil
		case "macos-application":
			return contract.DomainViewer, nil
		case "windows-application":
			return contract.DomainViewer, nil
		case "native-presentation", "unity", "unreal", "godot", "blender":
			return contract.DomainRender, nil
		default:
			return "", fmt.Errorf("Cymonkey cannot infer a domain from target.kind %q", target.Kind)
		}
	}
	if !contract.ValidDomain(requested) {
		return "", fmt.Errorf("unsupported Cymonkey domain %q", requested)
	}
	if requested == contract.DomainViewer && target.Kind != "browser" && target.Kind != "macos-application" && target.Kind != "windows-application" {
		return "", errors.New("Cymonkey viewer domain requires target.kind browser, macos-application, or windows-application")
	}
	if requested == contract.DomainRender && target.Kind != "browser" && target.Kind != "native-presentation" && target.Kind != "unity" && target.Kind != "unreal" && target.Kind != "godot" && target.Kind != "blender" {
		return "", errors.New("Cymonkey render domain requires target.kind browser, native-presentation, unity, unreal, or godot")
	}
	if requested == contract.DomainPlayer && target.Kind != "browser" && target.Kind != "macos-application" && target.Kind != "windows-application" && target.Kind != "native-presentation" && target.Kind != "unity" && target.Kind != "unreal" && target.Kind != "godot" && target.Kind != "blender" {
		return "", errors.New("Cymonkey player domain requires a browser, macos-application, windows-application, or native presentation target")
	}
	return requested, nil
}

func backendMatchesRequest(backend Backend, requested string) bool {
	// Driver presets name the client library that operates the target. The
	// selected backend remains the target protocol: Playwright attaches over
	// CDP, while Puppeteer can attach over either CDP or WebDriver BiDi.
	switch requested {
	case "playwright":
		return backend.Name() == BackendCDP
	case "puppeteer":
		return backend.Name() == BackendCDP || backend.Name() == BackendBiDi
	}
	if backend.Name() == BackendName(requested) {
		return true
	}
	if _, ok := backend.(enginePresentationBackend); ok {
		return strings.HasPrefix(requested, "engine-")
	}
	return false
}
