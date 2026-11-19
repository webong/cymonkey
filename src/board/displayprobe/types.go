package displayprobe

import "time"

// Observation reports host readiness without exposing native paths or content.
// Availability is an environment check, not proof of OS permission or a live capture.
type Observation struct {
	Platform      string              `json:"platform"`
	Containerized bool                `json:"containerized"`
	Native        bool                `json:"native"`
	Capabilities  []string            `json:"capabilities,omitempty"`
	X11           Endpoint            `json:"x11"`
	Wayland       Endpoint            `json:"wayland"`
	Recording     RecordingCapability `json:"recording"`
	Reason        string              `json:"reason,omitempty"`
	ObservedAt    time.Time           `json:"observedAt"`
}

// Endpoint reports whether a local display endpoint is present.
type Endpoint struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// RecordingCapability describes installed tools and capture prerequisites.
type RecordingCapability struct {
	Status           string    `json:"status"`
	EncoderAvailable bool      `json:"encoderAvailable"`
	CaptureAvailable bool      `json:"captureAvailable"`
	Permission       string    `json:"permission"`
	Formats          []string  `json:"formats,omitempty"`
	Message          string    `json:"message,omitempty"`
	CheckedAt        time.Time `json:"checkedAt"`
}
