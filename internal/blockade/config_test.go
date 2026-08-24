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
