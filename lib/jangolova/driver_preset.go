package jangolova

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"jangolova/sdk"
)

// DriverPreset selects a Jangolova automation backend with one
// driver pre-configured. Registered names such as "playwright" and
// "puppeteer" use presets so callers get semantic operations without
// configuring driver selection themselves.
type DriverPreset struct {
	adapter Adapter
	driver  string
}

// WithDriver returns a Jangolova adapter that pins backend selection
// to the given driver. Valid drivers: playwright, puppeteer, cdp, bidi.
func WithDriver(driver string, hosts ...sdk.Host) (DriverPreset, error) {
	var host sdk.Host
	if len(hosts) > 0 {
		host = hosts[0]
	}
	driver = strings.TrimSpace(driver)
	switch driver {
	case "playwright", "puppeteer", "cdp", "bidi":
		return DriverPreset{adapter: Adapter{Host: host}, driver: driver}, nil
	default:
		return DriverPreset{}, fmt.Errorf("unknown Cymonkey driver %q; valid drivers are playwright, puppeteer, cdp, bidi", driver)
	}
}

func (p DriverPreset) InspectEngine(ctx context.Context) sdk.EngineInspection {
	return p.adapter.InspectEngine(ctx)
}

func (p DriverPreset) Connect(ctx context.Context, spec sdk.EngineSpec, target sdk.EngineTarget) (sdk.EngineInstance, error) {
	options := map[string]any{}
	if len(spec.Options) > 0 && strings.TrimSpace(string(spec.Options)) != "{}" {
		if err := json.Unmarshal(spec.Options, &options); err != nil {
			return nil, fmt.Errorf("decode engine options: %w", err)
		}
	}
	if existing, ok := options["driver"].(string); ok {
		existing = strings.TrimSpace(existing)
		if existing != "" && existing != p.driver {
			return nil, fmt.Errorf("this adapter pins the Cymonkey %s driver; requested driver %q conflicts", p.driver, existing)
		}
	}
	if _, exists := options["driver"]; !exists {
		options["driver"] = p.driver
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	spec.Options = encoded
	return p.adapter.Connect(ctx, spec, target)
}
