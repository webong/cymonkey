// Package contracts defines Cymonkey's public wire types. Library-specific
// adapters translate these values at the composition boundary.
package contracts

const (
	ContentAPIVersion         = "content.item/v1"
	ContentAdapterVersion     = "content.adapter/v1"
	ObservationRequestVersion = "content.observation/v1"
	ObservationAPIVersion     = "content.observation/v1"
	ContentConfigVersion      = "cymonkey.content-config/v1alpha1"
	ModelManifestVersion      = "content.model/v1"
	RuntimeManifestVersion    = "content.runtime/v1"
	ContentServiceVersion     = "cymonkey.content-service/v1alpha1"
	ContentSourceVersion      = "content.source/v1"
)

// ContentConfig is Cymonkey's public configuration for content adapters.
// Plugin identifies an installed Cymonkey plugin; the internal library name
// and configuration shape never cross this boundary.
type ContentConfig struct {
	APIVersion string                 `yaml:"apiVersion" json:"apiVersion"`
	Adapters   []ContentAdapterConfig `yaml:"adapters" json:"adapters"`
}

type ContentAdapterConfig struct {
	ID              string                     `yaml:"id" json:"id"`
	Plugin          string                     `yaml:"plugin" json:"plugin"`
	Model           string                     `yaml:"model,omitempty" json:"model,omitempty"`
	ModelRoot       string                     `yaml:"modelRoot,omitempty" json:"modelRoot,omitempty"`
	Workers         int                        `yaml:"workers,omitempty" json:"workers,omitempty"`
	Timeout         string                     `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	MaxPayloadBytes int64                      `yaml:"maxPayloadBytes,omitempty" json:"maxPayloadBytes,omitempty"`
	Settings        map[string]string          `yaml:"settings,omitempty" json:"settings,omitempty"`
	Secrets         map[string]SecretReference `yaml:"secrets,omitempty" json:"secrets,omitempty"`
}

type SecretReference struct {
	Env string `yaml:"env" json:"env"`
}

type ContentPart struct {
	Kind      string `json:"kind"`
	Text      string `json:"text,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Data      []byte `json:"data,omitempty"`
}

type ContentRequest struct {
	APIVersion string        `json:"apiVersion,omitempty"`
	RequestID  string        `json:"requestId,omitempty"`
	Parts      []ContentPart `json:"parts"`
	Prompt     string        `json:"prompt,omitempty"`
}

type ContentFinding struct {
	Kind        string   `json:"kind"`
	Text        string   `json:"text"`
	SourceParts []int    `json:"sourceParts"`
	Confidence  *float64 `json:"confidence,omitempty"`
}

type ContentResponse struct {
	APIVersion string           `json:"apiVersion"`
	RequestID  string           `json:"requestId,omitempty"`
	Findings   []ContentFinding `json:"findings"`
}

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

type ObservationRequest struct {
	APIVersion string `json:"apiVersion,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	Image      []byte `json:"image"`
	Prompt     string `json:"prompt,omitempty"`
}

type ObservationResult struct {
	APIVersion   string        `json:"apiVersion"`
	RequestID    string        `json:"requestId,omitempty"`
	Observations []Observation `json:"observations"`
}
