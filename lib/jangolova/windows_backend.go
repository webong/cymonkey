package jangolova

import (
	"context"
	"errors"
	"fmt"
	"time"

	contract "cymonkey/src/jangolova/contract"
	"cymonkey/src/jangolova/sdk"
)

// windowsCooperativeBackend intentionally shares the authenticated helper
// transport with macOS. It does not call Win32 or inject input from the
// provider process; those operations stay inside the owner-launched helper.
type windowsCooperativeBackend struct{}

func (windowsCooperativeBackend) Name() BackendName { return BackendWindowsCooperative }
func (windowsCooperativeBackend) Domains() []contract.Domain {
	return []contract.Domain{contract.DomainViewer}
}
func (windowsCooperativeBackend) Compatible(target sdk.EngineTarget) bool {
	return target.Kind == "windows-application"
}

func (windowsCooperativeBackend) Connect(
	_ context.Context,
	spec sdk.EngineSpec,
	target sdk.EngineTarget,
	config Options,
) (sdk.EngineInstance, error) {
	if target.Kind != "windows-application" {
		return nil, errors.New("Cymonkey Windows backend requires target.kind windows-application")
	}
	if config.Host.ListenWebSocket == nil {
		return nil, errors.New("Jangolova host cooperative listener is required")
	}
	host, err := config.Host.ListenWebSocket(config.Native.ControlListen)
	if err != nil {
		return nil, fmt.Errorf("create Cymonkey Windows control host: %w", err)
	}
	running := &macOSInstance{
		host: host, policy: config.Policy, required: stableStrings(spec.RequiredCapabilities),
		events:  make(chan sdk.EngineEvent, 8),
		runtime: "windows-app", targetCapability: "target.windows-cooperative",
	}
	running.emit(sdk.EngineEvent{
		Type: "cymonkey.windows.awaiting_helper", Status: sdk.EngineHealthStarting, OccurredAt: time.Now().UTC(),
	})
	return running, nil
}
