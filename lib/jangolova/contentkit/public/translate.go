// Package contentbridge translates Cymonkey's public content contract to the
// internal interpretation library contract.
package public

import (
	"jangolova/contentkit"
)

func ToLibraryRequest(request ContentRequest) contentkit.InterpretRequest {
	parts := make([]contentkit.ContentPart, len(request.Parts))
	for index, part := range request.Parts {
		parts[index] = contentkit.ContentPart{Kind: part.Kind, Text: part.Text, MediaType: part.MediaType, Data: part.Data}
	}
	return contentkit.InterpretRequest{
		APIVersion: contentkit.ContentAPIVersion,
		RequestID:  request.RequestID,
		Parts:      parts,
		Prompt:     request.Prompt,
	}
}

func FromLibraryResponse(response contentkit.InterpretResponse) ContentResponse {
	findings := make([]ContentFinding, len(response.Findings))
	for index, finding := range response.Findings {
		findings[index] = ContentFinding{
			Kind: finding.Kind, Text: finding.Text,
			SourceParts: append([]int(nil), finding.SourceParts...), Confidence: finding.Confidence,
		}
	}
	return ContentResponse{
		APIVersion: ContentAPIVersion,
		RequestID:  response.RequestID,
		Findings:   findings,
	}
}

func FromLibraryObservation(response contentkit.ObserveResponse) ObservationResult {
	observations := make([]Observation, len(response.Observations))
	for index, observation := range response.Observations {
		observations[index] = Observation{
			Kind: observation.Kind, Label: observation.Label, Confidence: observation.Confidence,
			Region: Region{
				X: observation.Region.X, Y: observation.Region.Y,
				Width: observation.Region.Width, Height: observation.Region.Height,
			},
			Mask: observation.Mask, Evidence: observation.Evidence,
		}
	}
	return ObservationResult{
		APIVersion: ObservationAPIVersion,
		RequestID:  response.RequestID, Observations: observations,
	}
}
