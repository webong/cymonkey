package audio

// Request selects which devices must be present. The host authorizes access separately.
type Request struct {
	Playback      bool   `json:"playback,omitempty"`
	Microphone    bool   `json:"microphone,omitempty"`
	SystemCapture bool   `json:"systemCapture,omitempty"`
	OutputDevice  string `json:"outputDevice,omitempty"`
	InputDevice   string `json:"inputDevice,omitempty"`
}

type Device struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	State        string `json:"state,omitempty"`
	SampleFormat string `json:"sampleFormat,omitempty"`
	SampleRate   int    `json:"sampleRate,omitempty"`
	Channels     int    `json:"channels,omitempty"`
}

type Observation struct {
	Ready         bool     `json:"ready"`
	Backend       string   `json:"backend,omitempty"`
	Playback      bool     `json:"playback,omitempty"`
	Microphone    bool     `json:"microphone,omitempty"`
	SystemCapture bool     `json:"systemCapture,omitempty"`
	Devices       []Device `json:"devices,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}
