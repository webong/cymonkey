package blockade

import (
	"encoding/base64"
	"fmt"
)

// ValidateObservations checks observations against the
// blockade.observation/v1alpha1 contract. Provider adapters should call this
// before returning mapped results so malformed evidence never reaches agents.
func ValidateObservations(observations []Observation) error {
	for i, observation := range observations {
		if observation.Kind == "" {
			return fmt.Errorf("observation %d: kind is required", i)
		}
		if observation.Label == "" {
			return fmt.Errorf("observation %d (%s): label is required", i, observation.Kind)
		}
		if observation.Confidence < 0 || observation.Confidence > 1 {
			return fmt.Errorf("observation %d (%s): confidence %v outside [0,1]", i, observation.Label, observation.Confidence)
		}
		if observation.Region.Width < 0 || observation.Region.Height < 0 {
			return fmt.Errorf("observation %d (%s): region has negative dimensions", i, observation.Label)
		}
		if observation.Mask != "" && !validMaskPNG(observation.Mask) {
			return fmt.Errorf("observation %d (%s): mask is not base64-encoded PNG", i, observation.Label)
		}
	}
	return nil
}

// ValidateObserveResponse checks a full response against the observation
// contract, including the apiVersion marker.
func ValidateObserveResponse(response ObserveResponse) error {
	if response.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion %q must be %q", response.APIVersion, APIVersion)
	}
	return ValidateObservations(response.Observations)
}

func validMaskPNG(encoded string) bool {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) < 8 {
		return false
	}
	pngSignature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	for i, b := range pngSignature {
		if data[i] != b {
			return false
		}
	}
	return true
}
