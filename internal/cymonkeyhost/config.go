package cymonkeyhost

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const ConfigAPIVersion = "cymonkey.config/v1alpha1"

var componentIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// Config describes a Cymonkey host and the standalone subsystem processes it
// should run together. The host does not embed either subsystem; it only
// composes and supervises their executable boundaries.
type Config struct {
	APIVersion  string             `yaml:"apiVersion"`
	Kind        string             `yaml:"kind"`
	Components  []ComponentConfig  `yaml:"components"`
	Observation *ObservationConfig `yaml:"observation,omitempty"`
}

type ComponentConfig struct {
	ID               string            `yaml:"id"`
	Command          []string          `yaml:"command"`
	Environment      map[string]string `yaml:"environment,omitempty"`
	WorkingDirectory string            `yaml:"workingDirectory,omitempty"`
}

// ObservationConfig declares the two standalone services that Cymonkey
// coordinates for an observation. Jangolova supplies an authorized screenshot;
// Blockade receives pixels and returns its normalized result. Neither service
// needs configuration knowledge of the other.
type ObservationConfig struct {
	Jangolova ObservationJangolovaConfig `yaml:"jangolova"`
	Blockade  ObservationBlockadeConfig  `yaml:"blockade"`
}

type ObservationJangolovaConfig struct {
	Endpoint         string `yaml:"endpoint"`
	TokenEnvironment string `yaml:"tokenEnvironment"`
}

type ObservationBlockadeConfig struct {
	Endpoint string `yaml:"endpoint"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode Cymonkey host config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if c.APIVersion != ConfigAPIVersion {
		return fmt.Errorf("Cymonkey host config apiVersion must be %q", ConfigAPIVersion)
	}
	if strings.TrimSpace(c.Kind) != "CymonkeyHost" {
		return errors.New("Cymonkey host config kind must be \"CymonkeyHost\"")
	}
	if len(c.Components) == 0 {
		return errors.New("Cymonkey host config requires at least one component")
	}
	seen := make(map[string]struct{}, len(c.Components))
	for _, component := range c.Components {
		if !componentIDPattern.MatchString(component.ID) {
			return fmt.Errorf("Cymonkey component id %q is invalid", component.ID)
		}
		if _, exists := seen[component.ID]; exists {
			return fmt.Errorf("Cymonkey component id %q is duplicated", component.ID)
		}
		seen[component.ID] = struct{}{}
		if len(component.Command) == 0 || strings.TrimSpace(component.Command[0]) == "" {
			return fmt.Errorf("Cymonkey component %q requires a command", component.ID)
		}
		for index, value := range component.Command {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("Cymonkey component %q command argument %d is empty", component.ID, index)
			}
		}
	}
	if c.Observation != nil {
		if err := c.Observation.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c ObservationConfig) Validate() error {
	if err := validateHTTPURL("Cymonkey observation Jangolova endpoint", c.Jangolova.Endpoint); err != nil {
		return err
	}
	if strings.TrimSpace(c.Jangolova.TokenEnvironment) == "" {
		return errors.New("Cymonkey observation Jangolova tokenEnvironment is required")
	}
	return validateHTTPURL("Cymonkey observation Blockade endpoint", c.Blockade.Endpoint)
}

func validateHTTPURL(name, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an HTTP URL", name)
	}
	return nil
}
