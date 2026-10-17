package contentkit

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const ModelManifestAPIVersion = "content.model/v1"

var ModelNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var ModelVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)
var ModelDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ModelManifest describes files for one local model and the provider that can
// inspect and load them. A provider determines the files' actual model format.
type ModelManifest struct {
	APIVersion   string          `json:"apiVersion"`
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	Provider     string          `json:"provider"`
	Capabilities []string        `json:"capabilities"`
	Artifacts    []ModelArtifact `json:"artifacts"`
}

type ModelArtifact struct {
	Role   string `json:"role"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

// InstalledModel is passed to a provider for compatibility checks and at
// startup. Directory is absolute; artifact names are relative filenames.
type InstalledModel struct {
	Manifest  ModelManifest `json:"manifest"`
	Directory string        `json:"directory"`
}

func (m ModelManifest) Validate() error {
	if m.APIVersion != ModelManifestAPIVersion {
		return fmt.Errorf("model apiVersion must be %q", ModelManifestAPIVersion)
	}
	if !ModelNamePattern.MatchString(m.Name) || !ModelVersionPattern.MatchString(m.Version) || !ModelNamePattern.MatchString(m.Provider) {
		return errors.New("model name, version, or provider is invalid")
	}
	if len(m.Capabilities) == 0 || len(m.Capabilities) > 64 {
		return errors.New("model needs 1 to 64 capabilities")
	}
	seenCapabilities := make(map[string]bool, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		if capability == "" || len(capability) > 128 || strings.TrimSpace(capability) != capability || seenCapabilities[capability] {
			return errors.New("model capability is empty, too long, or duplicated")
		}
		seenCapabilities[capability] = true
	}
	if len(m.Artifacts) == 0 || len(m.Artifacts) > 32 {
		return errors.New("model needs 1 to 32 artifacts")
	}
	seenFiles := make(map[string]bool, len(m.Artifacts))
	for _, artifact := range m.Artifacts {
		if !ModelNamePattern.MatchString(artifact.Role) {
			return errors.New("model artifact role is invalid")
		}
		if artifact.File == "" || artifact.File == "." || artifact.File == ".." || artifact.File == "model.json" || filepath.Base(artifact.File) != artifact.File || strings.ContainsAny(artifact.File, `/\\`) || seenFiles[artifact.File] {
			return errors.New("model artifact must have a distinct filename")
		}
		if !ModelDigestPattern.MatchString(artifact.SHA256) {
			return fmt.Errorf("model artifact %q needs a lowercase SHA-256 digest", artifact.File)
		}
		seenFiles[artifact.File] = true
	}
	return nil
}
