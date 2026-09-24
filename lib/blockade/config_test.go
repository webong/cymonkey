package blockade

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigAndLocalWorkerConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blockade.yaml")
	content := []byte("apiVersion: blockade.config/v1alpha1\nengines:\n  - id: local\n    kind: local-ultralytics\n    command: [python3, worker.py]\n    workers: 2\n    yoloModel: /models/yolo.pt\n    samModel: /models/sam.pt\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	engine, ok := config.Engine("local")
	if !ok {
		t.Fatal("engine not found")
	}
	worker, err := engine.WorkerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if worker.Workers != 2 || len(worker.Command) != 2 {
		t.Fatalf("worker = %#v", worker)
	}
	if _, err := StartConfiguredLocalEngine(context.Background(), EngineConfig{Kind: "onnx"}); err == nil {
		t.Fatal("expected non-local engine error")
	}
}

func TestLoadOnnxExecutionProviderOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blockade.yaml")
	content := []byte("apiVersion: blockade.config/v1alpha1\nengines:\n  - id: nvidia\n    kind: onnx\n    yoloModel: /models/yolo.onnx\n    executionProviders:\n      - name: tensorrt\n        options:\n          device_id: '0'\n          trt_fp16_enable: '1'\n      - name: cuda\n        options:\n          device_id: '0'\n      - name: cpu\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	providers := config.Engines[0].ExecutionProviders
	if len(providers) != 3 || providers[0].Name != "tensorrt" || providers[1].Name != "cuda" || providers[2].Name != "cpu" {
		t.Fatalf("execution providers = %#v", providers)
	}
	if providers[0].Options["trt_fp16_enable"] != "1" {
		t.Fatalf("TensorRT options = %#v", providers[0].Options)
	}
}

func TestLoadProviderAdapterConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blockade.yaml")
	content := []byte("apiVersion: blockade.config/v1alpha1\nproviderAdapters:\n  - id: hosted-vision\n    kind: fixture\n    timeout: 45s\n    maxPayloadBytes: 1048576\n    settings:\n      model: fixture-v1\n      endpoint: https://provider.invalid\n    secrets:\n      apiToken:\n        env: BLOCKADE_HOSTED_VISION_TOKEN\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Engines) != 0 || len(config.ProviderAdapters) != 1 {
		t.Fatalf("config = %#v", config)
	}
	adapter := config.ProviderAdapters[0]
	if adapter.ID != "hosted-vision" || adapter.Settings["model"] != "fixture-v1" || adapter.Secrets["apiToken"].Env != "BLOCKADE_HOSTED_VISION_TOKEN" {
		t.Fatalf("adapter = %#v", adapter)
	}
	selected, ok := config.DefaultInference()
	if !ok || selected.ID() != adapter.ID || selected.ProviderAdapter == nil {
		t.Fatalf("default inference = %#v, ok = %v", selected, ok)
	}
	byID, ok := config.Inference(adapter.ID)
	if !ok || byID.ProviderAdapter == nil {
		t.Fatalf("inference lookup = %#v, ok = %v", byID, ok)
	}
}

func TestProviderAdapterConfigurationValidation(t *testing.T) {
	valid := func() Config {
		return Config{APIVersion: ConfigAPIVersion, ProviderAdapters: []ProviderAdapterConfig{{
			ID: "hosted", Kind: "fixture", Timeout: "30s", MaxPayloadBytes: 1024,
			Settings: map[string]string{"model": "fixture-v1"},
			Secrets:  map[string]SecretReference{"apiToken": {Env: "BLOCKADE_PROVIDER_TOKEN"}},
		}}}
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}

	tests := map[string]func(Config) Config{
		"missing kind": func(config Config) Config {
			config.ProviderAdapters[0].Kind = ""
			return config
		},
		"invalid timeout": func(config Config) Config {
			config.ProviderAdapters[0].Timeout = "forever"
			return config
		},
		"payload too large": func(config Config) Config {
			config.ProviderAdapters[0].MaxPayloadBytes = maximumProviderAdapterMaxPayloadBytes + 1
			return config
		},
		"invalid env reference": func(config Config) Config {
			config.ProviderAdapters[0].Secrets["apiToken"] = SecretReference{Env: "not valid"}
			return config
		},
		"plaintext credential setting": func(config Config) Config {
			config.ProviderAdapters[0].Settings["apiKey"] = "plaintext"
			return config
		},
		"duplicate engine id": func(config Config) Config {
			config.Engines = []EngineConfig{{ID: "hosted", Kind: "onnx", YOLOModel: "yolo.onnx"}}
			return config
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if err := mutate(valid()).Validate(); err == nil {
				t.Fatal("expected invalid provider-adapter configuration")
			}
		})
	}
}

func TestProviderAdapterConfigRejectsPlaintextSecretField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blockade.yaml")
	content := []byte("apiVersion: blockade.config/v1alpha1\nproviderAdapters:\n  - id: hosted\n    kind: fixture\n    secrets:\n      apiToken:\n        value: plaintext\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected plaintext secret field to be rejected")
	}
}

func TestExecutionProviderConfigurationValidation(t *testing.T) {
	validOnnx := func(providers ...ExecutionProviderConfig) Config {
		return Config{APIVersion: ConfigAPIVersion, Engines: []EngineConfig{{
			ID: "onnx-yolo", Kind: "onnx", YOLOModel: "yolo.onnx", ExecutionProviders: providers,
		}}}
	}
	for name, config := range map[string]Config{
		"defaults to CPU": validOnnx(),
		"ordered fallback": validOnnx(
			ExecutionProviderConfig{Name: " TensorRT "},
			ExecutionProviderConfig{Name: "CUDA"},
			ExecutionProviderConfig{Name: "cpu"},
		),
		"OpenVINO": validOnnx(ExecutionProviderConfig{Name: "openvino", Options: map[string]string{"device_type": "AUTO"}}),
		"CoreML":   validOnnx(ExecutionProviderConfig{Name: "coreml", Options: map[string]string{"MLComputeUnits": "ALL"}}),
	} {
		t.Run(name, func(t *testing.T) {
			if err := config.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}

	tests := map[string]Config{
		"unknown provider": validOnnx(ExecutionProviderConfig{Name: "metal"}),
		"duplicate provider": validOnnx(
			ExecutionProviderConfig{Name: "CUDA"},
			ExecutionProviderConfig{Name: "cuda"},
		),
		"CPU is not last": validOnnx(
			ExecutionProviderConfig{Name: "cpu"},
			ExecutionProviderConfig{Name: "openvino"},
		),
		"CPU options": validOnnx(ExecutionProviderConfig{Name: "cpu", Options: map[string]string{"threads": "2"}}),
		"local engine providers": {APIVersion: ConfigAPIVersion, Engines: []EngineConfig{{
			ID: "local", Kind: "local-ultralytics", Command: []string{"python3"}, YOLOModel: "yolo.pt", SAMModel: "sam.pt",
			ExecutionProviders: []ExecutionProviderConfig{{Name: "cpu"}},
		}}},
	}
	for name, config := range tests {
		t.Run(name, func(t *testing.T) {
			if err := config.Validate(); err == nil {
				t.Fatal("expected invalid execution-provider configuration")
			}
		})
	}
}

func TestConfiguredWorkerEndToEnd(t *testing.T) {
	dir := t.TempDir()
	yolo := filepath.Join(dir, "yolo.pt")
	sam := filepath.Join(dir, "sam.pt")
	if err := os.WriteFile(yolo, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sam, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{APIVersion: ConfigAPIVersion, Engines: []EngineConfig{{ID: "fixture", Kind: "local-ultralytics", Command: []string{os.Args[0], "-test.run=TestWorkerHelperProcess"}, Workers: 1, Environment: map[string]string{"BLOCKADE_WORKER_HELPER": "1"}, YOLOModel: yolo, SAMModel: sam}}}
	if err := config.ValidateModelFiles(); err != nil {
		t.Fatal(err)
	}
	pool, err := StartConfiguredLocalEngine(context.Background(), config.Engines[0])
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	result, err := (Client{WorkerPool: pool}).Observe(context.Background(), ObserveRequest{Image: []byte("fixture-image")})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Observations) != 1 || result.Observations[0].Label != "worker-object" {
		t.Fatalf("result = %#v", result)
	}
}
