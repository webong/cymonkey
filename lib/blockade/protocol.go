package blockade

import "encoding/json"

const APIVersion = "blockade.observation/v1alpha1"

type Region struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type Observation struct {
	Kind       string  `json:"kind"`
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	Region     Region  `json:"region"`
	Mask       string  `json:"mask,omitempty"`
	Evidence   string  `json:"evidence,omitempty"`
}

type ObserveRequest struct {
	APIVersion string `json:"apiVersion,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	Image      []byte `json:"image,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
}

type ObserveResponse struct {
	APIVersion   string        `json:"apiVersion"`
	RequestID    string        `json:"requestId,omitempty"`
	Observations []Observation `json:"observations"`
}

type CapabilitiesResponse struct {
	APIVersion   string   `json:"apiVersion"`
	Provider     string   `json:"provider"`
	Capabilities []string `json:"capabilities"`
}

func (r ObserveResponse) MarshalJSON() ([]byte, error) {
	type alias ObserveResponse
	if r.APIVersion == "" {
		r.APIVersion = APIVersion
	}
	return json.Marshal(alias(r))
}
