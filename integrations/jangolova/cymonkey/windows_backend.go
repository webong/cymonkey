package cymonkey

import (
	"context"
	"errors"
	"fmt"
	"time"

	"jangolova/internal/bridge"
	contract "jangolova/internal/cymonkey"
	"jangolova/internal/manifest"
	"jangolova/internal/orchestrator"
)

// windowsCooperativeBackend intentionally shares the authenticated helper
// transport with macOS. It does not call Win32 or inject input from the
// provider process; those operations stay inside the owner-launched helper.
type windowsCooperativeBackend struct{}

func (windowsCooperativeBackend) Name() BackendName       { return BackendWindowsCooperative }
func (windowsCooperativeBackend) Domain() contract.Domain { return contract.DomainViewer }
func (windowsCooperativeBackend) Compatible(target orchestrator.EngineTarget) bool {
	return target.Kind == "windows-application"
}

func (windowsCooperativeBackend) Connect(
	_ context.Context,
	spec manifest.EngineSpec,
	target orchestrator.EngineTarget,
	config options,
) (orchestrator.EngineInstance, error) {
	if target.Kind != "windows-application" {
		return nil, errors.New("Cymonkey Windows backend requires target.kind windows-application")
	}
	host, err := bridge.NewWebSocketHost(config.Native.ControlListen)
	if err != nil {
		return nil, fmt.Errorf("create Cymonkey Windows control host: %w", err)
	}
	running := &macOSInstance{
		host: host, policy: config.Policy, required: stableStrings(spec.RequiredCapabilities),
		events:  make(chan orchestrator.EngineEvent, 8),
		runtime: "windows-app", targetCapability: "target.windows-cooperative",
	}
	running.emit(orchestrator.EngineEvent{
		Type: "cymonkey.windows.awaiting_helper", Status: orchestrator.EngineHealthStarting, OccurredAt: time.Now().UTC(),
	})
	return running, nil
}
