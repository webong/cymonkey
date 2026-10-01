package cymonkeyhost

import (
	"bytes"
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
var environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Config describes a Cymonkey host and the standalone subsystem processes it
// should run together. The host does not embed either subsystem; it only
// composes and supervises their executable boundaries.
type Config struct {
	APIVersion string                   `yaml:"apiVersion"`
	Kind       string                   `yaml:"kind"`
	Components []ComponentConfig        `yaml:"components"`
	Services   map[string]ServiceConfig `yaml:"services,omitempty"`
}

type ComponentConfig struct {
	ID               string            `yaml:"id"`
	Command          []string          `yaml:"command"`
	Environment      map[string]string `yaml:"environment,omitempty"`
	WorkingDirectory string            `yaml:"workingDirectory,omitempty"`
}

// ServiceConfig connects a public service contract to a separately managed
// process. The host validates and supervises the connection without knowing
// which library implements the contract.
type ServiceConfig struct {
	Endpoint         string `yaml:"endpoint"`
	TokenEnvironment string `yaml:"tokenEnvironment,omitempty"`
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
	for contract, service := range c.Services {
		if !serviceContractPattern.MatchString(contract) {
			return fmt.Errorf("Cymonkey service contract %q is invalid", contract)
		}
		if err := service.Validate(contract); err != nil {
			return err
		}
	}
	return nil
}

var serviceContractPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)+$`)

func (c Config) Service(contract string) (ServiceConfig, bool) {
	service, ok := c.Services[strings.TrimSpace(contract)]
	return service, ok
}

func (c ServiceConfig) Validate(contract string) error {
	if err := validateHTTPURL(fmt.Sprintf("Cymonkey service %s endpoint", contract), c.Endpoint); err != nil {
		return err
	}
	if value := strings.TrimSpace(c.TokenEnvironment); value != "" && !environmentNamePattern.MatchString(value) {
		return fmt.Errorf("Cymonkey service %s tokenEnvironment is invalid", contract)
	}
	return nil
}

func validateHTTPURL(name, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an HTTP URL", name)
	}
	return nil
}
