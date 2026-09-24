package engineprovider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"cymonkey/src/internal/orchestrator"
)

// SelectAutomaticEngine chooses a stable available adapter from target
// protocols and caller-required capabilities. Endpoint location is ignored.
func SelectAutomaticEngine(
	ctx context.Context,
	registry *orchestrator.Registry,
	target Target,
	required []string,
) (string, error) {
	targetCapabilities := targetProtocolCapabilities(target.Endpoints)
	if len(targetCapabilities) == 0 {
		return "", fmt.Errorf("automatic engine selection requires at least one recognized target protocol")
	}
	for _, engine := range DiscoverEngines(ctx, registry) {
		if !engine.Available || !containsAny(engine.Capabilities, targetCapabilities) || !containsAll(engine.Capabilities, required) {
			continue
		}
		return engine.Adapter, nil
	}
	return "", fmt.Errorf(
		"no available engine matches target protocols [%s] and required capabilities [%s]",
		strings.Join(targetCapabilities, ", "),
		strings.Join(stableCapabilities(required), ", "),
	)
}

func targetProtocolCapabilities(endpoints []TargetEndpoint) []string {
	seen := make(map[string]struct{}, len(endpoints))
	values := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		protocol := strings.ToLower(strings.TrimSpace(endpoint.Protocol))
		capability := ""
		switch protocol {
		case "cdp":
			capability = "target.cdp"
		case "webdriver-bidi":
			capability = "target.webdriver-bidi"
		case "webdriver":
			capability = "target.webdriver"
		case "mcp-streamable-http":
			capability = "target.safari-mcp"
		case "vnc", "rfb":
			capability = "target.vnc"
		case "webrtc":
			capability = "target.webrtc"
		case "wayland-rfb":
			capability = "target.wayland-rfb"
		default:
			if protocol != "" {
				capability = "target." + protocol
			}
		}
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		values = append(values, capability)
	}
	sort.Strings(values)
	return values
}

func containsAny(values, candidates []string) bool {
	for _, candidate := range candidates {
		if containsString(values, candidate) {
			return true
		}
	}
	return false
}

func containsAll(values, required []string) bool {
	for _, requirement := range required {
		if !containsString(values, requirement) {
			return false
		}
	}
	return true
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
