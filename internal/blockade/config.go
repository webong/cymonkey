package blockade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const ConfigAPIVersion = "blockade.config/v1alpha1"

type Config struct {
	APIVersion string         `yaml:"apiVersion"`
	Engines    []EngineConfig `yaml:"engines"`
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

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
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
	if len(c.Engines) == 0 {
		return errors.New("Blockade config requires at least one engine")
	}
	seen := map[string]bool{}
	for _, engine := range c.Engines {
		if engine.ID == "" || seen[engine.ID] {
			return fmt.Errorf("Blockade engine id %q is missing or duplicated", engine.ID)
		}
		seen[engine.ID] = true
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
	return nil
}

// ValidateModelFiles verifies local model attachments without starting an
// engine. Hosted-provider and VLM integrations belong to Grimlock rather than
// Blockade configuration.
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
