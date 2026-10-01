// Package manifest contains interaction-engine configuration shared by
// Display adapters and transports consume these values. Target coordinates are
// intentionally carried separately by the caller-owned target contract.
package manifest

import "encoding/json"

type EngineSpec struct {
	Adapter              string          `json:"adapter"`
	RequiredCapabilities []string        `json:"requiredCapabilities,omitempty"`
	Source               string          `json:"source,omitempty"`
	Options              json.RawMessage `json:"options,omitempty"`
}
