package blockade

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const ConfigAPIVersion = "blockade.config/v1alpha1"

type Config struct {
	APIVersion string                  `yaml:"apiVersion" json:"apiVersion"`
	Providers  []ProviderAdapterConfig `yaml:"providers" json:"providers"`
}

// ProviderAdapterConfig configures one instance of an installed provider.
// Settings are non-secret provider configuration. Secrets can only name
// environment variables and are resolved lazily at runtime.
type ProviderAdapterConfig struct {
	ID              string                     `yaml:"id" json:"id"`
	Provider        string                     `yaml:"provider" json:"provider"`
	Model           string                     `yaml:"model,omitempty" json:"model,omitempty"`
	ModelRoot       string                     `yaml:"modelRoot,omitempty" json:"modelRoot,omitempty"`
	Workers         int                        `yaml:"workers,omitempty" json:"workers,omitempty"`
	Timeout         string                     `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	MaxPayloadBytes int64                      `yaml:"maxPayloadBytes,omitempty" json:"maxPayloadBytes,omitempty"`
	Settings        map[string]string          `yaml:"settings,omitempty" json:"settings,omitempty"`
	Secrets         map[string]SecretReference `yaml:"secrets,omitempty" json:"secrets,omitempty"`
}

type SecretReference struct {
	Env string `yaml:"env" json:"env"`
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

// SaveConfig validates and atomically writes a Blockade configuration. Files
// are private because they may reveal endpoint topology and secret variable
// names even though secret values are never stored in the configuration.
func SaveConfig(path string, config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("Blockade config path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	directory := filepath.Dir(absolute)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode Blockade config: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".blockade-config-*")
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
	if err := os.Rename(temporaryPath, absolute); err != nil {
		return err
	}
	return nil
}

func (c Config) Validate() error {
	if c.APIVersion != ConfigAPIVersion {
		return fmt.Errorf("Blockade config apiVersion must be %q", ConfigAPIVersion)
	}
	if len(c.Providers) == 0 {
		return errors.New("Blockade config requires at least one provider")
	}
	seen := map[string]bool{}
	for _, provider := range c.Providers {
		id := strings.TrimSpace(provider.ID)
		if id == "" || seen[id] {
			return fmt.Errorf("Blockade provider id %q is missing or duplicated", provider.ID)
		}
		seen[id] = true
		if err := provider.validate(); err != nil {
			return err
		}
	}
	return nil
}

// ValidateModelFiles verifies installed models without starting a provider.
func (c Config) ValidateModelFiles() error {
	if err := c.Validate(); err != nil {
		return err
	}
	for _, provider := range c.Providers {
		if provider.Model == "" {
			continue
		}
		root := provider.ModelRoot
		if root == "" {
			var err error
			root, err = DefaultModelRoot()
			if err != nil {
				return err
			}
		}
		if _, err := ResolveInstalledModel(root, canonicalProviderName(provider.Provider), provider.Model); err != nil {
			return fmt.Errorf("Blockade provider %q model %q: %w", provider.ID, provider.Model, err)
		}
	}
	return nil
}

func (c Config) Provider(id string) (ProviderAdapterConfig, bool) {
	id = strings.TrimSpace(id)
	for _, provider := range c.Providers {
		if strings.TrimSpace(provider.ID) == id {
			return provider, true
		}
	}
	return ProviderAdapterConfig{}, false
}

func (c Config) DefaultProvider() (ProviderAdapterConfig, bool) {
	if len(c.Providers) != 0 {
		return c.Providers[0], true
	}
	return ProviderAdapterConfig{}, false
}

func (c ProviderAdapterConfig) validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return errors.New("Blockade provider adapter id is required")
	}
	if !modelNamePattern.MatchString(canonicalProviderName(c.Provider)) {
		return fmt.Errorf("Blockade provider %q requires a valid provider name", c.ID)
	}
	if c.Model != "" {
		if _, _, err := parseModelReference(c.Model); err != nil {
			return fmt.Errorf("Blockade provider adapter %q: %w", c.ID, err)
		}
	}
	if c.ModelRoot != "" && !filepath.IsAbs(c.ModelRoot) {
		return fmt.Errorf("Blockade provider adapter %q modelRoot must be absolute", c.ID)
	}
	if c.Workers < 0 || c.Workers > 32 {
		return fmt.Errorf("Blockade provider adapter %q workers must be between 1 and 32 when set", c.ID)
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

func (c ProviderAdapterConfig) workers() int {
	if c.Workers == 0 {
		return 1
	}
	return c.Workers
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
