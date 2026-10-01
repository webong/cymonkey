package contracts

const DisplayModuleRegistryVersion = "cymonkey.display-modules/v1alpha1"

type DisplayModuleRegistry struct {
	SchemaVersion string          `json:"schemaVersion"`
	RegistryID    string          `json:"registryId"`
	Modules       []DisplayModule `json:"modules"`
}

type DisplayModule struct {
	ID              string                  `json:"id"`
	Version         string                  `json:"version"`
	Runtime         string                  `json:"runtime"`
	ProtocolVersion string                  `json:"protocolVersion"`
	Status          string                  `json:"status"`
	Platforms       []string                `json:"platforms,omitempty"`
	Actions         []string                `json:"actions,omitempty"`
	Artifacts       []DisplayModuleArtifact `json:"artifacts,omitempty"`
}

type DisplayModuleArtifact struct {
	Platform  string `json:"platform"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"mediaType,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

type DisplayModulePullResult struct {
	Module  string `json:"module"`
	Version string `json:"version"`
	Path    string `json:"path"`
}
