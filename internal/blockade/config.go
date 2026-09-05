package blockade

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const ConfigAPIVersion = "blockade.config/v1alpha1"

type Config struct {
	APIVersion       string                  `yaml:"apiVersion"`
	Engines          []EngineConfig          `yaml:"engines,omitempty"`
	ProviderAdapters []ProviderAdapterConfig `yaml:"providerAdapters,omitempty"`
}

type EngineConfig struct {
	ID                 string                    `yaml:"id"`
	Kind               string                    `yaml:"kind"` // local-ultralytics, onnx
	Workers            int                       `yaml:"workers,omitempty"`
	Command            []string                  `yaml:"command,omitempty"`
	Environment        map[string]string         `yaml:"environment,omitempty"`
	YOLOModel          string                    `yaml:"yoloModel,omitempty"`
	SAMModel           string                    `yaml:"samModel,omitempty"`
	ExecutionProviders []ExecutionProviderConfig `yaml:"executionProviders,omitempty"`
}

// ExecutionProviderConfig selects an ONNX Runtime execution provider. Entries
// are applied in order; ONNX Runtime uses later providers as fallbacks for
// nodes that an earlier provider cannot execute.
type ExecutionProviderConfig struct {
	Name    string            `yaml:"name"`
	Options map[string]string `yaml:"options,omitempty"`
}

// ProviderAdapterConfig selects a Blockade-owned hosted vision/VLM adapter.
// Settings are non-secret provider configuration. Secrets can only name
// environment variables and are resolved lazily at runtime.
type ProviderAdapterConfig struct {
	ID              string                     `yaml:"id"`
	Kind            string                     `yaml:"kind"`
	Timeout         string                     `yaml:"timeout,omitempty"`
	MaxPayloadBytes int64                      `yaml:"maxPayloadBytes,omitempty"`
	Settings        map[string]string          `yaml:"settings,omitempty"`
	Secrets         map[string]SecretReference `yaml:"secrets,omitempty"`
}

type SecretReference struct {
	Env string `yaml:"env"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode Blockade config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if c.APIVersion != ConfigAPIVersion {
		return fmt.Errorf("Blockade config apiVersion must be %q", ConfigAPIVersion)
	}
	if len(c.Engines) == 0 && len(c.ProviderAdapters) == 0 {
		return errors.New("Blockade config requires at least one engine or provider adapter")
	}
	seen := map[string]bool{}
	for _, engine := range c.Engines {
		id := strings.TrimSpace(engine.ID)
		if id == "" || seen[id] {
			return fmt.Errorf("Blockade engine id %q is missing or duplicated", engine.ID)
		}
		seen[id] = true
		switch engine.Kind {
		case "local-ultralytics":
			if len(engine.Command) == 0 {
				return fmt.Errorf("Blockade local engine %q requires command", engine.ID)
			}
			if engine.YOLOModel == "" || engine.SAMModel == "" {
				return fmt.Errorf("Blockade local engine %q requires yoloModel and samModel", engine.ID)
			}
			if len(engine.ExecutionProviders) != 0 {
				return fmt.Errorf("Blockade local engine %q cannot configure ONNX Runtime execution providers", engine.ID)
			}
		case "onnx":
			if engine.YOLOModel == "" && engine.SAMModel == "" {
				return fmt.Errorf("Blockade ONNX engine %q requires a model", engine.ID)
			}
			if err := validateExecutionProviders(engine.ID, engine.ExecutionProviders); err != nil {
				return err
			}
		default:
			return fmt.Errorf("Blockade engine %q has unsupported kind %q", engine.ID, engine.Kind)
		}
	}
	for _, adapter := range c.ProviderAdapters {
		id := strings.TrimSpace(adapter.ID)
		if id == "" || seen[id] {
			return fmt.Errorf("Blockade provider adapter id %q is missing or duplicated", adapter.ID)
		}
		seen[id] = true
		if err := adapter.validate(); err != nil {
			return err
		}
	}
	return nil
}

// ValidateModelFiles verifies local model attachments without starting an
// engine. Provider adapters have no local model files to validate.
func (c Config) ValidateModelFiles() error {
	if err := c.Validate(); err != nil {
		return err
	}
	for _, engine := range c.Engines {
		switch engine.Kind {
		case "local-ultralytics":
			for name, path := range map[string]string{"yoloModel": engine.YOLOModel, "samModel": engine.SAMModel} {
				if err := checkModelFile(engine.ID, name, path); err != nil {
					return err
				}
			}
		case "onnx":
			for name, path := range map[string]string{"yoloModel": engine.YOLOModel, "samModel": engine.SAMModel} {
				if path == "" {
					continue
				}
				if err := checkModelFile(engine.ID, name, path); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkModelFile(engineID, name, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("Blockade engine %q %s %q: %w", engineID, name, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("Blockade engine %q %s %q is a directory", engineID, name, path)
	}
	return nil
}

func (e EngineConfig) WorkerConfig() (WorkerConfig, error) {
	if e.Kind != "local-ultralytics" {
		return WorkerConfig{}, fmt.Errorf("engine %q is not a local Ultralytics engine", e.ID)
	}
	env := make([]string, 0, len(e.Environment)+2)
	for key, value := range e.Environment {
		env = append(env, key+"="+value)
	}
	env = append(env, "BLOCKADE_YOLO_MODEL="+e.YOLOModel, "BLOCKADE_SAM_MODEL="+e.SAMModel)
	return WorkerConfig{Command: e.Command, Workers: e.Workers, Env: env}, nil
}

func StartConfiguredLocalEngine(ctx context.Context, e EngineConfig) (*WorkerPool, error) {
	config, err := e.WorkerConfig()
	if err != nil {
		return nil, err
	}
	return NewWorkerPool(ctx, config)
}

func (c Config) Engine(id string) (EngineConfig, bool) {
	for _, engine := range c.Engines {
		if strings.TrimSpace(engine.ID) == id {
			return engine, true
		}
	}
	return EngineConfig{}, false
}

type InferenceConfig struct {
	Engine          *EngineConfig
	ProviderAdapter *ProviderAdapterConfig
}

func (c Config) Inference(id string) (InferenceConfig, bool) {
	id = strings.TrimSpace(id)
	if engine, ok := c.Engine(id); ok {
		return InferenceConfig{Engine: &engine}, true
	}
	for _, adapter := range c.ProviderAdapters {
		if strings.TrimSpace(adapter.ID) == id {
			copy := adapter
			return InferenceConfig{ProviderAdapter: &copy}, true
		}
	}
	return InferenceConfig{}, false
}

func (c Config) DefaultInference() (InferenceConfig, bool) {
	if len(c.Engines) != 0 {
		engine := c.Engines[0]
		return InferenceConfig{Engine: &engine}, true
	}
	if len(c.ProviderAdapters) != 0 {
		adapter := c.ProviderAdapters[0]
		return InferenceConfig{ProviderAdapter: &adapter}, true
	}
	return InferenceConfig{}, false
}

func (c InferenceConfig) ID() string {
	if c.Engine != nil {
		return c.Engine.ID
	}
	if c.ProviderAdapter != nil {
		return c.ProviderAdapter.ID
	}
	return ""
}

func (c ProviderAdapterConfig) validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return errors.New("Blockade provider adapter id is required")
	}
	if canonicalAdapterKind(c.Kind) == "" {
		return fmt.Errorf("Blockade provider adapter %q requires kind", c.ID)
	}
	if _, err := c.parsedTimeout(); err != nil {
		return err
	}
	if c.MaxPayloadBytes < 0 || c.MaxPayloadBytes > maximumProviderAdapterMaxPayloadBytes {
		return fmt.Errorf("Blockade provider adapter %q maxPayloadBytes must be between 1 and %d when set", c.ID, maximumProviderAdapterMaxPayloadBytes)
	}
	for key := range c.Settings {
		if plaintextCredentialSetting(key) {
			return fmt.Errorf("Blockade provider adapter %q setting %q looks secret; use an environment reference under secrets", c.ID, key)
		}
	}
	for name, reference := range c.Secrets {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("Blockade provider adapter %q has an empty secret name", c.ID)
		}
		if !environmentVariableName.MatchString(reference.Env) {
			return fmt.Errorf("Blockade provider adapter %q secret %q requires a valid env reference", c.ID, name)
		}
	}
	return nil
}

func (c ProviderAdapterConfig) parsedTimeout() (time.Duration, error) {
	if strings.TrimSpace(c.Timeout) == "" {
		return defaultProviderAdapterTimeout, nil
	}
	timeout, err := time.ParseDuration(c.Timeout)
	if err != nil || timeout <= 0 || timeout > maximumProviderAdapterTimeout {
		return 0, fmt.Errorf("Blockade provider adapter %q timeout must be a duration between 1ns and %s", c.ID, maximumProviderAdapterTimeout)
	}
	return timeout, nil
}

func (c ProviderAdapterConfig) timeout() time.Duration {
	timeout, _ := c.parsedTimeout()
	return timeout
}

func (c ProviderAdapterConfig) maxPayloadBytes() int64 {
	if c.MaxPayloadBytes == 0 {
		return defaultProviderAdapterMaxPayloadBytes
	}
	return c.MaxPayloadBytes
}

var environmentVariableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func plaintextCredentialSetting(key string) bool {
	key = strings.ToLower(key)
	key = strings.NewReplacer("_", "", "-", "", ".", "").Replace(key)
	switch key {
	case "key", "apikey", "token", "accesstoken", "authtoken", "bearertoken", "password", "secret", "apisecret", "clientsecret", "privatekey", "signingkey", "credential", "credentials", "authorization":
		return true
	default:
		return false
	}
}
