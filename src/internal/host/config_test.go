package cymonkeyhost

import (
	"os"
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		APIVersion: ConfigAPIVersion,
		Kind:       "CymonkeyHost",
		Components: []ComponentConfig{{ID: "content", Command: []string{"cymy", "content", "serve"}}},
	}
}

func TestConfigValidate(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Config){
		"api version":   func(c *Config) { c.APIVersion = "wrong/v1" },
		"kind":          func(c *Config) { c.Kind = "Other" },
		"no components": func(c *Config) { c.Components = nil },
		"bad id":        func(c *Config) { c.Components[0].ID = "Blockade" },
		"empty command": func(c *Config) { c.Components[0].Command = nil },
	} {
		t.Run(name, func(t *testing.T) {
			config := validConfig()
			mutate(&config)
			if err := config.Validate(); err == nil {
				t.Fatal("Validate() unexpectedly succeeded")
			}
		})
	}
}

func TestServiceConfigValidate(t *testing.T) {
	config := validConfig()
	config.Services = map[string]ServiceConfig{
		"display.interaction":    {Endpoint: "http://127.0.0.1:7391", TokenEnvironment: "CYMONKEY_DISPLAY_TOKEN"},
		"content.interpretation": {Endpoint: "http://127.0.0.1:8091", TokenEnvironment: "CYMONKEY_CONTENT_TOKEN"},
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	service := config.Services["content.interpretation"]
	service.Endpoint = "not-an-endpoint"
	config.Services["content.interpretation"] = service
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() unexpectedly accepted an invalid service endpoint")
	}
	service.Endpoint = "http://127.0.0.1:8091"
	service.TokenEnvironment = "not valid"
	config.Services["content.interpretation"] = service
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() unexpectedly accepted an invalid service token environment")
	}
	config.Services = map[string]ServiceConfig{"Not Valid": {Endpoint: "http://127.0.0.1:8091"}}
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() unexpectedly accepted an invalid service contract")
	}
}

func TestLoadConfig(t *testing.T) {
	path := t.TempDir() + "/cymonkey.yaml"
	contents := `apiVersion: cymonkey.config/v1alpha1
kind: CymonkeyHost
components:
  - id: content
    command: [cymy, content, serve]
    environment:
      CYMONKEY_CONTENT_BIND: 127.0.0.1:8091
`
	if err := writeFile(path, contents); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Components[0].Environment["CYMONKEY_CONTENT_BIND"] != "127.0.0.1:8091" {
		t.Fatalf("config = %#v", config)
	}
}

func TestLoadConfigRejectsLibraryNamedObservationServices(t *testing.T) {
	path := t.TempDir() + "/cymonkey.yaml"
	contents := `apiVersion: cymonkey.config/v1alpha1
kind: CymonkeyHost
components:
  - id: service
    command: [service]
observation:
  blockade:
    endpoint: http://127.0.0.1:8091
`
	if err := writeFile(path, contents); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("accepted the removed library-named observation configuration")
	}
}

func writeFile(path, contents string) error {
	return os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o600)
}
