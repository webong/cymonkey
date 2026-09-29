package blockade

import (
	"context"
	"errors"
	"fmt"
	"math"
	"mime"
	"strings"
	"unicode/utf8"
)

const ContentAPIVersion = "blockade.content/v1alpha1"

const (
	CapabilityTextInterpret       = "text.interpret"
	CapabilityAudioInterpret      = "audio.interpret"
	CapabilityImageInterpret      = "image.interpret"
	CapabilityMultimodalInterpret = "multimodal.interpret"
	maximumContentParts           = 16
	maximumContentBytes           = 10 << 20
	maximumFindingTextBytes       = 1 << 20
	maximumContentResponseBytes   = 4 << 20
)

// ContentPart is caller-supplied content, not a path or target handle. Binary
// data is base64-encoded by JSON. Providers decide which formats they support.
type ContentPart struct {
	Kind      string `json:"kind"` // text, image, audio
	Text      string `json:"text,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Data      []byte `json:"data,omitempty"`
}

type InterpretRequest struct {
	APIVersion string        `json:"apiVersion,omitempty"`
	RequestID  string        `json:"requestId,omitempty"`
	Parts      []ContentPart `json:"parts"`
	Prompt     string        `json:"prompt,omitempty"`
}

// ContentFinding is one model-produced interpretation. SourceParts indexes
// into the request's parts so callers can retain provenance across modalities.
type ContentFinding struct {
	Kind        string   `json:"kind"`
	Text        string   `json:"text"`
	SourceParts []int    `json:"sourceParts"`
	Confidence  *float64 `json:"confidence,omitempty"`
}

type InterpretResponse struct {
	APIVersion string           `json:"apiVersion"`
	RequestID  string           `json:"requestId,omitempty"`
	Findings   []ContentFinding `json:"findings"`
}

// ContentEngine is optional. Existing image-only engines continue to expose
// Observe without claiming text or audio interpretation.
type ContentEngine interface {
	Interpret(context.Context, InterpretRequest) (InterpretResponse, error)
}

var ErrModalityUnsupported = errors.New("Blockade backend does not support the requested modality")

func ValidateInterpretRequest(request InterpretRequest) error {
	if request.APIVersion != ContentAPIVersion {
		return fmt.Errorf("apiVersion %q must be %q", request.APIVersion, ContentAPIVersion)
	}
	if len(request.Parts) == 0 || len(request.Parts) > maximumContentParts {
		return fmt.Errorf("content request must contain 1 to %d parts", maximumContentParts)
	}
	if len(request.RequestID) > 128 || !utf8.ValidString(request.RequestID) || len(request.Prompt) > 16<<10 || !utf8.ValidString(request.Prompt) {
		return errors.New("content request ID or prompt is invalid")
	}
	size := len(request.Prompt)
	for i, part := range request.Parts {
		size += len(part.Text) + len(part.Data)
		if size > maximumContentBytes {
			return fmt.Errorf("content request exceeds %d bytes", maximumContentBytes)
		}
		switch part.Kind {
		case "text":
			if strings.TrimSpace(part.Text) == "" || !utf8.ValidString(part.Text) || part.MediaType != "" || len(part.Data) != 0 {
				return fmt.Errorf("part %d must contain text only", i)
			}
		case "image", "audio":
			if part.Text != "" || len(part.Data) == 0 {
				return fmt.Errorf("part %d must contain binary data only", i)
			}
			mediaType, _, err := mime.ParseMediaType(part.MediaType)
			if err != nil || !strings.HasPrefix(mediaType, part.Kind+"/") {
				return fmt.Errorf("part %d needs an %s media type", i, part.Kind)
			}
		default:
			return fmt.Errorf("part %d has unsupported kind %q", i, part.Kind)
		}
	}
	return nil
}

func ValidateInterpretResponse(response InterpretResponse, request InterpretRequest) error {
	if response.APIVersion != ContentAPIVersion {
		return fmt.Errorf("apiVersion %q must be %q", response.APIVersion, ContentAPIVersion)
	}
	if !utf8.ValidString(response.RequestID) || len(response.RequestID) > 128 || (request.RequestID != "" && response.RequestID != request.RequestID) {
		return errors.New("content response requestId does not match request")
	}
	if response.Findings == nil || len(response.Findings) > 128 {
		return errors.New("content response findings must be an array of at most 128 items")
	}
	totalText := 0
	for i, finding := range response.Findings {
		totalText += len(finding.Kind) + len(finding.Text)
		if totalText > maximumContentResponseBytes {
			return errors.New("content response exceeds the output limit")
		}
		if strings.TrimSpace(finding.Kind) == "" || !utf8.ValidString(finding.Kind) || strings.TrimSpace(finding.Text) == "" || !utf8.ValidString(finding.Text) || len(finding.Text) > maximumFindingTextBytes {
			return fmt.Errorf("finding %d has invalid kind or text", i)
		}
		if len(finding.SourceParts) == 0 || len(finding.SourceParts) > len(request.Parts) {
			return fmt.Errorf("finding %d has no source parts", i)
		}
		seen := map[int]bool{}
		for _, index := range finding.SourceParts {
			if index < 0 || index >= len(request.Parts) || seen[index] {
				return fmt.Errorf("finding %d has an invalid source part", i)
			}
			seen[index] = true
		}
		if finding.Confidence != nil && (math.IsNaN(*finding.Confidence) || math.IsInf(*finding.Confidence, 0) || *finding.Confidence < 0 || *finding.Confidence > 1) {
			return fmt.Errorf("finding %d confidence is outside [0,1]", i)
		}
	}
	return nil
}

func requiredContentCapability(parts []ContentPart) string {
	seen := map[string]bool{}
	for _, part := range parts {
		seen[part.Kind] = true
	}
	if len(seen) > 1 {
		return CapabilityMultimodalInterpret
	}
	if seen["text"] {
		return CapabilityTextInterpret
	}
	if seen["audio"] {
		return CapabilityAudioInterpret
	}
	return CapabilityImageInterpret
}
