package contentengine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProviderConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blockade.yaml")
	content := []byte("apiVersion: blockade.config/v1alpha1\nproviders:\n  - id: hosted-vision\n    provider: fixture\n    timeout: 45s\n    maxPayloadBytes: 1048576\n    settings:\n      model: fixture-v1\n      endpoint: https://provider.invalid\n    secrets:\n      apiToken:\n        env: BLOCKADE_HOSTED_VISION_TOKEN\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Providers) != 1 {
		t.Fatalf("config = %#v", config)
	}
	provider := config.Providers[0]
	if provider.ID != "hosted-vision" || provider.Settings["model"] != "fixture-v1" || provider.Secrets["apiToken"].Env != "BLOCKADE_HOSTED_VISION_TOKEN" {
		t.Fatalf("provider = %#v", provider)
	}
	selected, ok := config.DefaultProvider()
	if !ok || selected.ID != provider.ID {
		t.Fatalf("default provider = %#v, ok = %v", selected, ok)
	}
	byID, ok := config.Provider(provider.ID)
	if !ok || byID.ID != provider.ID {
		t.Fatalf("provider lookup = %#v, ok = %v", byID, ok)
	}
}

func TestProviderSettingsRemainOpaqueToBlockade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blockade.yaml")
	content := []byte("apiVersion: blockade.config/v1alpha1\nproviders:\n  - id: nvidia\n    provider: onnx\n    model: yolo@1.0.0\n    settings:\n      executionProviders: '[{\"name\":\"tensorrt\"},{\"name\":\"cuda\"},{\"name\":\"cpu\"}]'\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Providers[0].Settings["executionProviders"]; got != `[{"name":"tensorrt"},{"name":"cuda"},{"name":"cpu"}]` {
		t.Fatalf("executionProviders = %q", got)
	}
}

func TestProviderModelReferenceValidation(t *testing.T) {
	valid := Config{APIVersion: ConfigAPIVersion, Providers: []ProviderAdapterConfig{{
		ID: "local", Provider: "onnx", Model: "detector@1.0.0", ModelRoot: t.TempDir(),
	}}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalidReference := valid
	invalidReference.Providers = append([]ProviderAdapterConfig(nil), valid.Providers...)
	invalidReference.Providers[0].Model = "detector"
	if err := invalidReference.Validate(); err == nil {
		t.Fatal("expected invalid model reference")
	}
	invalidRoot := valid
	invalidRoot.Providers = append([]ProviderAdapterConfig(nil), valid.Providers...)
	invalidRoot.Providers[0].ModelRoot = "relative/models"
	if err := invalidRoot.Validate(); err == nil {
		t.Fatal("expected relative model root to fail")
	}
}

func TestProviderConfigurationValidation(t *testing.T) {
	valid := func() Config {
		return Config{APIVersion: ConfigAPIVersion, Providers: []ProviderAdapterConfig{{
			ID: "hosted", Provider: "fixture", Timeout: "30s", MaxPayloadBytes: 1024,
			Settings: map[string]string{"model": "fixture-v1"},
			Secrets:  map[string]SecretReference{"apiToken": {Env: "BLOCKADE_PROVIDER_TOKEN"}},
		}}}
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(Config) Config{
		"missing provider": func(config Config) Config {
			config.Providers[0].Provider = ""
			return config
		},
		"invalid timeout": func(config Config) Config {
			config.Providers[0].Timeout = "forever"
			return config
		},
		"payload too large": func(config Config) Config {
			config.Providers[0].MaxPayloadBytes = maximumProviderAdapterMaxPayloadBytes + 1
			return config
		},
		"invalid env reference": func(config Config) Config {
			config.Providers[0].Secrets["apiToken"] = SecretReference{Env: "not valid"}
			return config
		},
		"plaintext credential setting": func(config Config) Config {
			config.Providers[0].Settings["apiKey"] = "plaintext"
			return config
		},
		"duplicate id": func(config Config) Config {
			config.Providers = append(config.Providers, ProviderAdapterConfig{ID: "hosted", Provider: "other"})
			return config
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if err := mutate(valid()).Validate(); err == nil {
				t.Fatal("expected invalid provider configuration")
			}
		})
	}
}

func TestConfigRejectsLegacyEngines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blockade.yaml")
	content := []byte("apiVersion: blockade.config/v1alpha1\nengines:\n  - id: local\n    kind: local-ultralytics\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected legacy engines configuration to be rejected")
	}
}

func TestConfigRejectsLegacyProviderKind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blockade.yaml")
	content := []byte("apiVersion: blockade.config/v1alpha1\nproviders:\n  - id: local\n    kind: onnx\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected legacy provider kind to be rejected")
	}
}

func TestProviderConfigRejectsPlaintextSecretField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blockade.yaml")
	content := []byte("apiVersion: blockade.config/v1alpha1\nproviders:\n  - id: hosted\n    provider: fixture\n    secrets:\n      apiToken:\n        value: plaintext\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected plaintext secret field to be rejected")
	}
}

func TestSaveConfigRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "blockade.yaml")
	config := Config{APIVersion: ConfigAPIVersion, Providers: []ProviderAdapterConfig{{
		ID: "hosted", Provider: "openai-compatible", Settings: map[string]string{"model": "fixture"},
	}}}
	if err := SaveConfig(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Providers[0].Settings["model"] != "fixture" {
		t.Fatalf("saved config = %#v", loaded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o", info.Mode().Perm())
	}
}
