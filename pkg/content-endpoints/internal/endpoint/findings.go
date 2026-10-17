package endpoint

import (
	"encoding/json"
	"errors"
	"fmt"
	"jangolova/contentkit"
	"strings"
)

const ResponseInstruction = `Return only a JSON object with a "findings" array. Each finding must contain "kind", "text", and "sourceParts". sourceParts contains zero-based indexes of the supplied content parts. Optional confidence is a number from 0 to 1.`

func Normalize(content string, request contentkit.InterpretRequest) (contentkit.ProviderAdapterInterpretResponse, error) {
	content = stripCodeFence(strings.TrimSpace(content))
	if content == "" {
		return contentkit.ProviderAdapterInterpretResponse{}, contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidResponse, errors.New("endpoint returned empty model content"))
	}
	var envelope struct {
		Findings []contentkit.ContentFinding `json:"findings"`
	}
	if json.Unmarshal([]byte(content), &envelope) == nil && envelope.Findings != nil {
		response := contentkit.InterpretResponse{
			APIVersion: contentkit.ContentAPIVersion,
			RequestID:  request.RequestID,
			Findings:   envelope.Findings,
		}
		if err := contentkit.ValidateInterpretResponse(response, request); err != nil {
			return contentkit.ProviderAdapterInterpretResponse{}, contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidResponse, fmt.Errorf("endpoint findings: %w", err))
		}
		return contentkit.ProviderAdapterInterpretResponse{APIVersion: contentkit.ProviderAdapterAPIVersion, Response: response}, nil
	}

	sources := make([]int, len(request.Parts))
	for index := range sources {
		sources[index] = index
	}
	response := contentkit.InterpretResponse{
		APIVersion: contentkit.ContentAPIVersion,
		RequestID:  request.RequestID,
		Findings: []contentkit.ContentFinding{{
			Kind: "response", Text: content, SourceParts: sources,
		}},
	}
	if err := contentkit.ValidateInterpretResponse(response, request); err != nil {
		return contentkit.ProviderAdapterInterpretResponse{}, contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidResponse, err)
	}
	return contentkit.ProviderAdapterInterpretResponse{APIVersion: contentkit.ProviderAdapterAPIVersion, Response: response}, nil
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
