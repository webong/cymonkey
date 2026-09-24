package webpresentation

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const presentationArtifactAPIVersion = "interaction.presentation/v1alpha1"

var (
	artifactIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
	artifactKindPattern = regexp.MustCompile(`^[a-z][a-z0-9.-]*$`)
)

type artifactLocation struct {
	Transport string `json:"transport"`
	URI       string `json:"uri"`
	MediaType string `json:"mediaType,omitempty"`
	Audience  string `json:"audience,omitempty"`
}

type presentationArtifact struct {
	APIVersion string             `json:"apiVersion"`
	ArtifactID string             `json:"artifactId"`
	Revision   string             `json:"revision"`
	Kind       string             `json:"kind"`
	Integrity  string             `json:"integrity,omitempty"`
	ByteLength *int64             `json:"byteLength,omitempty"`
	Locations  []artifactLocation `json:"locations"`
	Metadata   json.RawMessage    `json:"metadata,omitempty"`
}

type artifactMountInput struct {
	ExpectedStateRevision string               `json:"expectedStateRevision"`
	Artifact              presentationArtifact `json:"artifact"`
}

func (p presentationPolicy) validateArtifactMount(raw json.RawMessage) error {
	var input artifactMountInput
	if err := strictDecode(raw, &input); err != nil {
		return fmt.Errorf("decode presentation.mount input: %w", err)
	}
	if strings.TrimSpace(input.ExpectedStateRevision) == "" {
		return errors.New("presentation.mount expectedStateRevision is required")
	}
	artifact := input.Artifact
	if artifact.APIVersion != presentationArtifactAPIVersion {
		return fmt.Errorf("presentation artifact apiVersion must be %q", presentationArtifactAPIVersion)
	}
	if !artifactIDPattern.MatchString(artifact.ArtifactID) || len(artifact.ArtifactID) > 256 {
		return errors.New("presentation artifactId is invalid")
	}
	if strings.TrimSpace(artifact.Revision) == "" || len(artifact.Revision) > 256 {
		return errors.New("presentation artifact revision is required and must not exceed 256 bytes")
	}
	if !artifactKindPattern.MatchString(artifact.Kind) || len(artifact.Kind) > 128 {
		return errors.New("presentation artifact kind is invalid")
	}
	if artifact.Kind != "web.entrypoint" {
		return fmt.Errorf("web presentation does not support artifact kind %q", artifact.Kind)
	}
	if artifact.ByteLength != nil && *artifact.ByteLength < 0 {
		return errors.New("presentation artifact byteLength must not be negative")
	}
	if len(artifact.Locations) == 0 || len(artifact.Locations) > 16 {
		return errors.New("presentation artifact requires between 1 and 16 locations")
	}

	var locationErrors []string
	for _, location := range artifact.Locations {
		if !stringAllowed(location.Transport, p.AllowedArtifactTransports) {
			continue
		}
		if err := p.validateArtifactLocation(location); err != nil {
			locationErrors = append(locationErrors, err.Error())
			continue
		}
		return nil
	}
	if len(locationErrors) > 0 {
		return fmt.Errorf("presentation artifact has no valid allowed location: %s", strings.Join(locationErrors, "; "))
	}
	return fmt.Errorf("presentation artifact has no location using an allowed transport (%s)", strings.Join(p.AllowedArtifactTransports, ", "))
}

func (p presentationPolicy) validateArtifactLocation(location artifactLocation) error {
	parsed, err := url.Parse(location.URI)
	if err != nil {
		return fmt.Errorf("parse artifact location %q: %w", location.URI, err)
	}
	switch location.Transport {
	case "http", "https":
		if parsed.Scheme != location.Transport || parsed.Host == "" {
			return fmt.Errorf("artifact transport %q requires a matching absolute URI", location.Transport)
		}
		return p.validateSource(location.URI)
	case "target-file":
		if parsed.Scheme != "file" || !strings.HasPrefix(parsed.Path, "/") {
			return errors.New("artifact transport target-file requires an absolute file URI")
		}
		return nil
	default:
		return fmt.Errorf("web presentation does not support artifact transport %q", location.Transport)
	}
}

func stringAllowed(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
