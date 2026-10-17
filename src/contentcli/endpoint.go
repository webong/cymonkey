package contentcli

import (
	"contentkit"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"blockade"
)

var openAIEndpointProfiles = []string{"openai", "vllm", "sglang", "ollama", "llama.cpp", "lm-studio", "custom"}

func DefaultConfigPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("CYMONKEY_CONTENT_CONFIG")); value != "" {
		return filepath.Abs(value)
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "cymonkey", "content.yaml"), nil
}

func endpointCommand(args []string, stdout io.Writer, registry *contentkit.ProviderAdapterRegistry, resolver contentkit.SecretResolver, invocation, defaultConfig string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s endpoint requires add, list, remove, or test", invocation)
	}
	switch args[0] {
	case "help", "-h", "--help":
		_, err := fmt.Fprintf(stdout, "Usage: %s endpoint <add|list|remove|test> [flags]\n", invocation)
		return err
	case "add":
		return endpointAddCommand(args[1:], stdout, invocation, defaultConfig)
	case "list":
		return endpointListCommand(args[1:], stdout, invocation, defaultConfig)
	case "remove":
		return endpointRemoveCommand(args[1:], stdout, invocation, defaultConfig)
	case "test":
		return endpointTestCommand(args[1:], stdout, registry, resolver, invocation, defaultConfig)
	default:
		return fmt.Errorf("unknown %s endpoint command %q", invocation, args[0])
	}
}

func endpointAddCommand(args []string, stdout io.Writer, invocation, defaultConfig string) error {
	flags := flag.NewFlagSet(invocation+" endpoint add", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfig, "content YAML config")
	id := flags.String("id", "", "endpoint id")
	provider := flags.String("provider", "", "provider package name")
	profile := flags.String("profile", "", "openai, vllm, sglang, ollama, llama.cpp, lm-studio, custom, anthropic, google, or gemini")
	baseURL := flags.String("url", "", "endpoint base URL")
	model := flags.String("model", "", "endpoint model name")
	modalities := flags.String("modalities", "text", "comma-separated text,image,audio,video allow-list")
	apiKeyEnvironment := flags.String("api-key-env", "", "environment variable containing the endpoint API key")
	timeout := flags.String("timeout", "", "provider request timeout")
	workers := flags.Int("workers", 0, "provider process count")
	maxPayloadBytes := flags.Int64("max-payload-bytes", 0, "maximum request payload bytes")
	maxTokens := flags.Int("max-tokens", 0, "maximum generated tokens")
	temperature := flags.String("temperature", "", "model temperature from 0 to 2")
	replace := flags.Bool("replace", false, "replace an existing endpoint with the same id")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*id) == "" || strings.TrimSpace(*model) == "" {
		return fmt.Errorf("%s endpoint add requires --id and --model", invocation)
	}
	providerName, profileSetting, err := endpointProvider(strings.TrimSpace(*provider), strings.TrimSpace(*profile))
	if err != nil {
		return err
	}
	modalityList, err := normalizeModalities(*modalities)
	if err != nil {
		return err
	}
	settings := map[string]string{
		"model":      strings.TrimSpace(*model),
		"modalities": strings.Join(modalityList, ","),
	}
	if profileSetting != "" {
		settings["profile"] = profileSetting
	}
	if value := strings.TrimSpace(*baseURL); value != "" {
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("--url must be an absolute HTTP(S) URL without credentials, query, or fragment")
		}
		settings["baseURL"] = value
	}
	if *maxTokens != 0 {
		if *maxTokens < 1 || *maxTokens > 65536 {
			return errors.New("--max-tokens must be between 1 and 65536")
		}
		settings["maxTokens"] = strconv.Itoa(*maxTokens)
	}
	if value := strings.TrimSpace(*temperature); value != "" {
		parsed, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 || parsed > 2 {
			return errors.New("--temperature must be a number from 0 to 2")
		}
		settings["temperature"] = value
	}
	configured := contentkit.ProviderAdapterConfig{
		ID: strings.TrimSpace(*id), Provider: providerName, Workers: *workers,
		Timeout: strings.TrimSpace(*timeout), MaxPayloadBytes: *maxPayloadBytes, Settings: settings,
	}
	if name := strings.TrimSpace(*apiKeyEnvironment); name != "" {
		configured.Secrets = map[string]contentkit.SecretReference{"apiKey": {Env: name}}
	}
	config, err := loadConfigForEdit(*configPath)
	if err != nil {
		return err
	}
	replaced := false
	for index := range config.Providers {
		if config.Providers[index].ID != configured.ID {
			continue
		}
		if !*replace {
			return fmt.Errorf("content endpoint %q is already configured; use --replace to update it", configured.ID)
		}
		config.Providers[index] = configured
		replaced = true
		break
	}
	if !replaced {
		config.Providers = append(config.Providers, configured)
	}
	if err := contentkit.SaveConfig(*configPath, config); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"config": *configPath, "endpoint": configured})
}

func endpointProvider(provider, profile string) (string, string, error) {
	profile = strings.ToLower(profile)
	if provider == "" {
		switch {
		case profile == "anthropic":
			return "anthropic-compatible", "", nil
		case profile == "google" || profile == "gemini":
			return "google-compatible", "", nil
		case slices.Contains(openAIEndpointProfiles, profile):
			return "openai-compatible", profile, nil
		case profile == "":
			return "", "", errors.New("endpoint add requires --profile or --provider")
		default:
			return "", "", fmt.Errorf("unknown endpoint profile %q", profile)
		}
	}
	provider = strings.ToLower(provider)
	if profile == "" {
		return provider, "", nil
	}
	if provider != "openai-compatible" || !slices.Contains(openAIEndpointProfiles, profile) {
		return "", "", errors.New("--profile can only select an OpenAI-compatible profile when --provider is set")
	}
	return provider, profile, nil
}

func normalizeModalities(value string) ([]string, error) {
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if !slices.Contains([]string{"text", "image", "audio", "video"}, item) || slices.Contains(result, item) {
			return nil, fmt.Errorf("unsupported or duplicate modality %q", item)
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil, errors.New("at least one modality is required")
	}
	return result, nil
}

func endpointListCommand(args []string, stdout io.Writer, invocation, defaultConfig string) error {
	flags := flag.NewFlagSet(invocation+" endpoint list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfig, "content YAML config")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%s endpoint list accepts flags only", invocation)
	}
	config, err := contentkit.LoadConfig(*configPath)
	if errors.Is(err, os.ErrNotExist) {
		return json.NewEncoder(stdout).Encode(map[string]any{"config": *configPath, "endpoints": []contentkit.ProviderAdapterConfig{}})
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"config": *configPath, "endpoints": config.Providers})
}

func endpointRemoveCommand(args []string, stdout io.Writer, invocation, defaultConfig string) error {
	flags := flag.NewFlagSet(invocation+" endpoint remove", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfig, "content YAML config")
	id := flags.String("id", "", "endpoint id")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*id) == "" {
		return fmt.Errorf("%s endpoint remove requires --id", invocation)
	}
	config, err := contentkit.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	index := -1
	for candidate := range config.Providers {
		if config.Providers[candidate].ID == strings.TrimSpace(*id) {
			index = candidate
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("content endpoint %q is not configured", strings.TrimSpace(*id))
	}
	config.Providers = append(config.Providers[:index], config.Providers[index+1:]...)
	if len(config.Providers) == 0 {
		if err := os.Remove(*configPath); err != nil {
			return err
		}
	} else if err := contentkit.SaveConfig(*configPath, config); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "removed content endpoint %s\n", strings.TrimSpace(*id))
	return err
}

func endpointTestCommand(args []string, stdout io.Writer, registry *contentkit.ProviderAdapterRegistry, resolver contentkit.SecretResolver, invocation, defaultConfig string) error {
	flags := flag.NewFlagSet(invocation+" endpoint test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfig, "content YAML config")
	id := flags.String("id", "", "endpoint id; defaults to the first configured endpoint")
	liveText := flags.String("text", "", "optional text for a live inference request")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%s endpoint test accepts flags only", invocation)
	}
	config, err := contentkit.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	selected, err := selectProvider(config, *id)
	if err != nil {
		return err
	}
	engine, err := contentkit.StartConfiguredInference(context.Background(), selected, registry, resolver)
	if err != nil {
		return err
	}
	defer engine.Close()
	health := contentkit.EngineHealth{Ready: true}
	if reporter, ok := engine.(contentkit.EngineHealthReporter); ok {
		health, err = reporter.Health(context.Background())
		if err != nil {
			return err
		}
	}
	capabilities := []string{contentkit.CapabilityImageObserve}
	if reporter, ok := engine.(contentkit.EngineCapabilities); ok {
		capabilities, err = reporter.Capabilities(context.Background())
		if err != nil {
			return err
		}
	}
	result := map[string]any{"id": selected.ID, "provider": selected.Provider, "ready": health.Ready, "capabilities": capabilities}
	if health.Detail != "" {
		result["detail"] = health.Detail
	}
	if text := strings.TrimSpace(*liveText); text != "" {
		response, callErr := (blockade.Client{Engine: engine}).Interpret(context.Background(), contentkit.InterpretRequest{
			APIVersion: contentkit.ContentAPIVersion,
			Parts:      []contentkit.ContentPart{{Kind: "text", Text: text}},
			Prompt:     "Return a concise finding confirming this endpoint is reachable.",
		})
		if callErr != nil {
			return callErr
		}
		result["response"] = response
	}
	return json.NewEncoder(stdout).Encode(result)
}

func loadConfigForEdit(path string) (contentkit.Config, error) {
	config, err := contentkit.LoadConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return contentkit.Config{APIVersion: contentkit.ConfigAPIVersion}, nil
	}
	return config, err
}
