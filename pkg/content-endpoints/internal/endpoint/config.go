package endpoint

import (
	"contentkit"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type Config struct {
	BaseURL      *url.URL
	Model        string
	Modalities   []string
	Capabilities []string
	MaxTokens    int
	Temperature  *float64
}

type ParseOptions struct {
	DefaultBaseURL    string
	DefaultModalities []string
	AllowedModalities []string
	ExtraSettings     []string
}

func ParseConfig(settings map[string]string, options ParseOptions) (Config, error) {
	allowed := map[string]bool{
		"baseURL": true, "model": true, "modalities": true,
		"maxTokens": true, "temperature": true,
	}
	for _, name := range options.ExtraSettings {
		allowed[name] = true
	}
	for name := range settings {
		if !allowed[name] {
			return Config{}, fmt.Errorf("unsupported provider setting %q", name)
		}
	}

	base := strings.TrimSpace(settings["baseURL"])
	if base == "" {
		base = options.DefaultBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Config{}, errors.New("baseURL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")

	model := strings.TrimSpace(settings["model"])
	if model == "" || len(model) > 512 {
		return Config{}, errors.New("model is required and must be at most 512 bytes")
	}

	modalities := append([]string(nil), options.DefaultModalities...)
	if raw := strings.TrimSpace(settings["modalities"]); raw != "" {
		modalities = nil
		for _, value := range strings.Split(raw, ",") {
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "" || !slices.Contains(options.AllowedModalities, value) || slices.Contains(modalities, value) {
				return Config{}, fmt.Errorf("modalities contains unsupported or duplicate value %q", value)
			}
			modalities = append(modalities, value)
		}
	}
	if len(modalities) == 0 {
		return Config{}, errors.New("at least one modality is required")
	}

	maxTokens := 1024
	if raw := strings.TrimSpace(settings["maxTokens"]); raw != "" {
		maxTokens, err = strconv.Atoi(raw)
		if err != nil || maxTokens < 1 || maxTokens > 65536 {
			return Config{}, errors.New("maxTokens must be an integer from 1 to 65536")
		}
	}

	var temperature *float64
	if raw := strings.TrimSpace(settings["temperature"]); raw != "" {
		value, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 2 {
			return Config{}, errors.New("temperature must be a number from 0 to 2")
		}
		temperature = &value
	}

	capabilities := make([]string, 0, len(modalities)+1)
	for _, modality := range modalities {
		switch modality {
		case "text":
			capabilities = append(capabilities, contentkit.CapabilityTextInterpret)
		case "image":
			capabilities = append(capabilities, contentkit.CapabilityImageInterpret)
		case "audio":
			capabilities = append(capabilities, contentkit.CapabilityAudioInterpret)
		case "video":
			capabilities = append(capabilities, contentkit.CapabilityVideoInterpret)
		}
	}
	if len(modalities) > 1 {
		capabilities = append(capabilities, contentkit.CapabilityMultimodalInterpret)
	}
	return Config{
		BaseURL: parsed, Model: model, Modalities: modalities,
		Capabilities: capabilities, MaxTokens: maxTokens, Temperature: temperature,
	}, nil
}

func (c Config) URL(path string) string {
	copy := *c.BaseURL
	copy.Path = strings.TrimRight(copy.Path, "/") + "/" + strings.TrimLeft(path, "/")
	return copy.String()
}

func (c Config) ValidateRequest(request contentkit.InterpretRequest) error {
	for _, part := range request.Parts {
		if !slices.Contains(c.Modalities, part.Kind) {
			return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, fmt.Errorf("configured endpoint does not support %s input", part.Kind))
		}
	}
	return nil
}
