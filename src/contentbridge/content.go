// Package contentbridge translates Cymonkey's public content contract to the
// internal interpretation library contract.
package contentbridge

import (
	"contentkit"
	"cymonkey/src/contracts"
)

func ToLibraryRequest(request contracts.ContentRequest) contentkit.InterpretRequest {
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

func FromLibraryResponse(response contentkit.InterpretResponse) contracts.ContentResponse {
	findings := make([]contracts.ContentFinding, len(response.Findings))
	for index, finding := range response.Findings {
		findings[index] = contracts.ContentFinding{
			Kind: finding.Kind, Text: finding.Text,
			SourceParts: append([]int(nil), finding.SourceParts...), Confidence: finding.Confidence,
		}
	}
	return contracts.ContentResponse{
		APIVersion: contracts.ContentAPIVersion,
		RequestID:  response.RequestID,
		Findings:   findings,
	}
}

func FromLibraryObservation(response contentkit.ObserveResponse) contracts.ObservationResult {
	observations := make([]contracts.Observation, len(response.Observations))
	for index, observation := range response.Observations {
		observations[index] = contracts.Observation{
			Kind: observation.Kind, Label: observation.Label, Confidence: observation.Confidence,
			Region: contracts.Region{
				X: observation.Region.X, Y: observation.Region.Y,
				Width: observation.Region.Width, Height: observation.Region.Height,
			},
			Mask: observation.Mask, Evidence: observation.Evidence,
		}
	}
	return contracts.ObservationResult{
		APIVersion: contracts.ObservationAPIVersion,
		RequestID:  response.RequestID, Observations: observations,
	}
}
