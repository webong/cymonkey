package endpoint

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"blockade"
)

const ResponseInstruction = `Return only a JSON object with a "findings" array. Each finding must contain "kind", "text", and "sourceParts". sourceParts contains zero-based indexes of the supplied content parts. Optional confidence is a number from 0 to 1.`

func Normalize(content string, request blockade.InterpretRequest) (blockade.ProviderAdapterInterpretResponse, error) {
	content = stripCodeFence(strings.TrimSpace(content))
	if content == "" {
		return blockade.ProviderAdapterInterpretResponse{}, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidResponse, errors.New("endpoint returned empty model content"))
	}
	var envelope struct {
		Findings []blockade.ContentFinding `json:"findings"`
	}
	if json.Unmarshal([]byte(content), &envelope) == nil && envelope.Findings != nil {
		response := blockade.InterpretResponse{
			APIVersion: blockade.ContentAPIVersion,
			RequestID:  request.RequestID,
			Findings:   envelope.Findings,
		}
		if err := blockade.ValidateInterpretResponse(response, request); err != nil {
			return blockade.ProviderAdapterInterpretResponse{}, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidResponse, fmt.Errorf("endpoint findings: %w", err))
		}
		return blockade.ProviderAdapterInterpretResponse{APIVersion: blockade.ProviderAdapterAPIVersion, Response: response}, nil
	}

	sources := make([]int, len(request.Parts))
	for index := range sources {
		sources[index] = index
	}
	response := blockade.InterpretResponse{
		APIVersion: blockade.ContentAPIVersion,
		RequestID:  request.RequestID,
		Findings: []blockade.ContentFinding{{
			Kind: "response", Text: content, SourceParts: sources,
		}},
	}
	if err := blockade.ValidateInterpretResponse(response, request); err != nil {
		return blockade.ProviderAdapterInterpretResponse{}, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidResponse, err)
	}
	return blockade.ProviderAdapterInterpretResponse{APIVersion: blockade.ProviderAdapterAPIVersion, Response: response}, nil
}

func stripCodeFence(value string) string {
	if !strings.HasPrefix(value, "```") || !strings.HasSuffix(value, "```") {
		return value
	}
	value = strings.TrimPrefix(value, "```")
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		value = value[index+1:]
	}
	value = strings.TrimSuffix(value, "```")
	return strings.TrimSpace(value)
}
