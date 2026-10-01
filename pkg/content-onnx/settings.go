package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	defaultConfidenceThreshold = 0.25
	defaultNMSThreshold        = 0.45
)

type executionProviderConfig struct {
	Name    string            `json:"name"`
	Options map[string]string `json:"options,omitempty"`
}

type providerSettings struct {
	executionProviders []executionProviderConfig
	runtimeLibrary     string
	confidence         float64
	iou                float64
}

func parseProviderSettings(settings map[string]string) (providerSettings, error) {
	result := providerSettings{confidence: defaultConfidenceThreshold, iou: defaultNMSThreshold}
	for key, value := range settings {
		switch key {
		case "executionProviders":
			if err := json.Unmarshal([]byte(value), &result.executionProviders); err != nil {
				return providerSettings{}, fmt.Errorf("decode executionProviders: %w", err)
			}
		case "runtimeLibrary":
			result.runtimeLibrary = strings.TrimSpace(value)
		case "confidenceThreshold":
			threshold, err := parseThreshold(key, value)
			if err != nil {
				return providerSettings{}, err
			}
			result.confidence = threshold
		case "nmsThreshold":
			threshold, err := parseThreshold(key, value)
			if err != nil {
				return providerSettings{}, err
			}
			result.iou = threshold
		default:
			return providerSettings{}, fmt.Errorf("unsupported ONNX provider setting %q", key)
		}
	}
	if result.runtimeLibrary != "" && !strings.HasPrefix(result.runtimeLibrary, "/") {
		return providerSettings{}, errors.New("runtimeLibrary must be an absolute path")
	}
	if err := validateExecutionProviders(result.executionProviders); err != nil {
		return providerSettings{}, err
	}
	return result, nil
}

func parseThreshold(name, value string) (float64, error) {
	threshold, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || threshold < 0 || threshold > 1 {
		return 0, fmt.Errorf("%s must be a number in [0,1]", name)
	}
	return threshold, nil
}
