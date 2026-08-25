package cymonkey

import (
	"context"
	"errors"
	"fmt"

	contract "jangolova/internal/cymonkey"
	"jangolova/internal/manifest"
	"jangolova/internal/orchestrator"
)

type processBackend struct {
	name             BackendName
	endpointProtocol string
}

func (backend processBackend) Name() BackendName         { return backend.name }
func (backend processBackend) Profile() contract.Profile { return contract.ProfileWeb }
func (backend processBackend) Compatible(target orchestrator.EngineTarget) bool {
	_, ok := target.Endpoint(backend.endpointProtocol)
	return target.Kind == "browser" && ok
}

var configuredBackends = []Backend{
	processBackend{name: BackendCDP, endpointProtocol: "cdp"},
	processBackend{name: BackendBiDi, endpointProtocol: "webdriver-bidi"},
	safariMCPBackend{},
	macOSCooperativeBackend{},
	enginePresentationBackend{},
}

func (Adapter) Connect(ctx context.Context, spec manifest.EngineSpec, target orchestrator.EngineTarget) (orchestrator.EngineInstance, error) {
	config, err := decodeOptions(spec.Options)
	if err != nil {
		return nil, err
	}
	profile, err := resolveProfile(config.Profile, target)
	if err != nil {
		return nil, err
	}
	backend, err := selectBackendForProfile(config.Backend, profile, target)
	if err != nil {
		return nil, err
	}
	if profile != contract.ProfileWeb && config.Extension.Mode == extensionRequired {
		return nil, errors.New("Cymonkey WebExtension mode is available only for the web profile")
	}
	if backend.Name() != BackendCDP && config.Extension.Mode == extensionRequired {
		return nil, fmt.Errorf("Cymonkey extension mode required is not available with backend %s", backend.Name())
	}
	return backend.Connect(ctx, spec, target, config)
}

func selectBackend(requested string, target orchestrator.EngineTarget) (Backend, error) {
	return selectBackendForProfile(requested, contract.ProfileWeb, target)
}

func selectBackendForProfile(requested string, profile contract.Profile, target orchestrator.EngineTarget) (Backend, error) {
	for _, backend := range configuredBackends {
		if backend.Profile() != profile {
			continue
		}
		if requested != "auto" && requested != string(backend.Name()) {
			continue
		}
		if backend.Compatible(target) {
			return backend, nil
		}
	}
	if requested == "auto" {
		if profile == contract.ProfileMacOS {
			return nil, errors.New("Cymonkey macOS profile requires a caller-owned native helper; Apple Events and Accessibility are not invoked directly by the provider")
		}
		if profile == contract.ProfileEngine {
			return nil, errors.New("Cymonkey engine profile requires a caller-owned websocket or pacman-ws presentation endpoint")
		}
		return nil, errors.New("Cymonkey web profile requires a caller-owned CDP, WebDriver BiDi, or Safari MCP endpoint")
	}
	return nil, fmt.Errorf("Cymonkey backend %s has no compatible caller-owned %s target", requested, profile)
}

func resolveProfile(requested contract.Profile, target orchestrator.EngineTarget) (contract.Profile, error) {
	if requested == "" {
		switch target.Kind {
		case "browser":
			return contract.ProfileWeb, nil
		case "macos-application":
			return contract.ProfileMacOS, nil
		case "native-presentation", "unity", "unreal", "godot":
			return contract.ProfileEngine, nil
		default:
			return "", fmt.Errorf("Cymonkey cannot infer a profile from target.kind %q", target.Kind)
		}
	}
	if !contract.ValidProfile(requested) {
		return "", fmt.Errorf("unsupported Cymonkey profile %q", requested)
	}
	if requested == contract.ProfileWeb && target.Kind != "browser" {
		return "", errors.New("Cymonkey web profile requires target.kind browser")
	}
	if requested == contract.ProfileMacOS && target.Kind != "macos-application" {
		return "", errors.New("Cymonkey macOS profile requires target.kind macos-application")
	}
	if requested == contract.ProfileEngine && target.Kind != "native-presentation" && target.Kind != "unity" && target.Kind != "unreal" && target.Kind != "godot" {
		return "", errors.New("Cymonkey engine profile requires target.kind native-presentation, unity, unreal, or godot")
	}
	return requested, nil
}
