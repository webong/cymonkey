package contentbridge

import (
	"bytes"
	"console/contentengine"
	"cymonkey/src/contracts"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxConfigBytes = 1 << 20

func ToLibraryConfig(config contracts.ContentConfig) contentengine.Config {
	adapters := make([]contentengine.ProviderAdapterConfig, len(config.Adapters))
	for index, adapter := range config.Adapters {
		secrets := make(map[string]contentengine.SecretReference, len(adapter.Secrets))
		for name, reference := range adapter.Secrets {
			secrets[name] = contentengine.SecretReference{Env: reference.Env}
		}
		adapters[index] = contentengine.ProviderAdapterConfig{
			ID: adapter.ID, Provider: adapter.Plugin, Model: adapter.Model,
			ModelRoot: adapter.ModelRoot, Workers: adapter.Workers,
			Timeout: adapter.Timeout, MaxPayloadBytes: adapter.MaxPayloadBytes,
			Settings: cloneStrings(adapter.Settings), Secrets: secrets,
		}
	}
	return contentengine.Config{APIVersion: contentengine.ConfigAPIVersion, Providers: adapters}
}

func FromLibraryConfig(config contentengine.Config) contracts.ContentConfig {
	adapters := make([]contracts.ContentAdapterConfig, len(config.Providers))
	for index, adapter := range config.Providers {
		secrets := make(map[string]contracts.SecretReference, len(adapter.Secrets))
		for name, reference := range adapter.Secrets {
			secrets[name] = contracts.SecretReference{Env: reference.Env}
		}
		adapters[index] = contracts.ContentAdapterConfig{
			ID: adapter.ID, Plugin: adapter.Provider, Model: adapter.Model,
			ModelRoot: adapter.ModelRoot, Workers: adapter.Workers,
			Timeout: adapter.Timeout, MaxPayloadBytes: adapter.MaxPayloadBytes,
			Settings: cloneStrings(adapter.Settings), Secrets: secrets,
		}
	}
	return contracts.ContentConfig{APIVersion: contracts.ContentConfigVersion, Adapters: adapters}
}

func LoadConfig(path string) (contracts.ContentConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return contracts.ContentConfig{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return contracts.ContentConfig{}, err
	}
	if len(data) > maxConfigBytes {
		return contracts.ContentConfig{}, errors.New("content config exceeds 1 MiB")
	}
	var config contracts.ContentConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return contracts.ContentConfig{}, fmt.Errorf("decode content config: %w", err)
	}
	if err := ValidateConfig(config); err != nil {
		return contracts.ContentConfig{}, err
	}
	return config, nil
}

func SaveConfig(path string, config contracts.ContentConfig) error {
	if err := ValidateConfig(config); err != nil {
		return err
	}
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("content config path is required")
	}
	directory := filepath.Dir(absolute)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode content config: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".content-config-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, absolute)
}

func ValidateConfig(config contracts.ContentConfig) error {
	if config.APIVersion != contracts.ContentConfigVersion {
		return fmt.Errorf("content config apiVersion must be %q", contracts.ContentConfigVersion)
	}
	if len(config.Adapters) == 0 {
		return errors.New("content config requires at least one adapter")
	}
	if err := ToLibraryConfig(config).Validate(); err != nil {
		return publicError(err)
	}
	return nil
}

func cloneStrings(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func publicError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.NewReplacer(
		"Blockade provider adapter", "content adapter",
		"Blockade provider", "content adapter",
		"Blockade config", "content config",
		"blockade", "content",
	).Replace(err.Error())
	return errors.New(message)
}
