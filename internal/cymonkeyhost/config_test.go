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
		Components: []ComponentConfig{{ID: "blockade", Command: []string{"blockade", "serve"}}},
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

func TestObservationConfigValidate(t *testing.T) {
	config := validConfig()
	config.Observation = &ObservationConfig{
		Jangolova: ObservationJangolovaConfig{Endpoint: "http://127.0.0.1:7391", TokenEnvironment: "CYMONKEY_JANGOLOVA_TOKEN"},
		Blockade:  ObservationBlockadeConfig{Endpoint: "http://127.0.0.1:8091"},
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.Observation.Blockade.Endpoint = "not-an-endpoint"
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() unexpectedly accepted an invalid Blockade endpoint")
	}
}

func TestLoadConfig(t *testing.T) {
	path := t.TempDir() + "/cymonkey.yaml"
	contents := `apiVersion: cymonkey.config/v1alpha1
kind: CymonkeyHost
components:
  - id: blockade
    command: [blockade, serve]
    environment:
      BLOCKADE_BIND: 127.0.0.1:8091
`
	if err := writeFile(path, contents); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Components[0].Environment["BLOCKADE_BIND"] != "127.0.0.1:8091" {
		t.Fatalf("config = %#v", config)
	}
}

func writeFile(path, contents string) error {
	return os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o600)
}
