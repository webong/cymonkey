package blockade

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLocalUltralyticsFixtureSmoke(t *testing.T) {
	if os.Getenv("JANGOLOVA_BLOCKADE_SMOKE") != "1" {
		t.Skip("set JANGOLOVA_BLOCKADE_SMOKE=1 with local YOLO/SAM weights to run the inference smoke test")
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repository root for fixture smoke test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	modelCache := filepath.Join(root, ".cache", "blockade", "models")
	yoloModel := filepath.Join(modelCache, envDefault("BLOCKADE_YOLO_MODEL_FILE", "yolo11n.pt"))
	samModel := filepath.Join(modelCache, envDefault("BLOCKADE_SAM_MODEL_FILE", "sam2_b.pt"))
	for _, model := range []string{yoloModel, samModel} {
		if _, err := os.Stat(model); err != nil {
			t.Skipf("fixture weights unavailable: %v", err)
		}
	}
	python := os.Getenv("JANGOLOVA_BLOCKADE_PYTHON")
	if python == "" {
		python = "python3"
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Skipf("fixture interpreter unavailable: %v", err)
	}
	engine := EngineConfig{
		ID:        "smoke-local-yolo-sam",
		Kind:      "local-ultralytics",
		Command:   []string{python, filepath.Join(root, "deploy", "blockade", "worker.py")},
		Workers:   1,
		YOLOModel: yoloModel,
		SAMModel:  samModel,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := StartConfiguredLocalEngine(ctx, engine)
	if err != nil {
		t.Fatalf("start Blockade workers: %v", err)
	}
	defer pool.Close()
	image, err := os.ReadFile(filepath.Join(root, "internal", "blockade", "testdata", "smoke.png"))
	if err != nil {
		t.Fatal(err)
	}
	client := Client{WorkerPool: pool}
	result, err := client.Observe(ctx, ObserveRequest{RequestID: "smoke-1", Image: image})
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	assertValidObservationResponse(t, result, "smoke-1")
	t.Logf("fixture observations: %d", len(result.Observations))
	broken, err := client.Observe(ctx, ObserveRequest{RequestID: "smoke-2", Image: []byte("not-an-image")})
	if err == nil {
		t.Fatalf("expected error for undecodable image, got %#v", broken)
	}
}

func TestOnnxEngineFixtureSmoke(t *testing.T) {
	if os.Getenv("JANGOLOVA_BLOCKADE_SMOKE") != "1" {
		t.Skip("set JANGOLOVA_BLOCKADE_SMOKE=1 with an exported ONNX model to run the inference smoke test")
	}
	modelPath := os.Getenv("JANGOLOVA_BLOCKADE_ONNX_MODEL")
	runtimeLib := os.Getenv("JANGOLOVA_BLOCKADE_ONNXRUNTIME_LIB")
	if modelPath == "" || runtimeLib == "" {
		t.Skip("JANGOLOVA_BLOCKADE_ONNX_MODEL and JANGOLOVA_BLOCKADE_ONNXRUNTIME_LIB are required for the ONNX smoke test")
	}
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("ONNX model unavailable: %v", err)
	}
	if _, err := os.Stat(runtimeLib); err != nil {
		t.Skipf("ONNX Runtime library unavailable: %v", err)
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repository root for fixture smoke test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	image, err := os.ReadFile(filepath.Join(root, "internal", "blockade", "testdata", "smoke.png"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	started, err := StartConfiguredEngine(ctx, EngineConfig{
		ID:        "smoke-onnx-yolo",
		Kind:      "onnx",
		YOLOModel: modelPath,
	})
	if err != nil {
		t.Fatalf("start ONNX engine: %v", err)
	}
	defer started.Close()
	result, err := (Client{Engine: started}).Observe(ctx, ObserveRequest{RequestID: "onnx-smoke-1", Image: image})
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	assertValidObservationResponse(t, result, "onnx-smoke-1")
	t.Logf("onnx fixture observations: %d", len(result.Observations))
}

func assertValidObservationResponse(t *testing.T, result ObserveResponse, requestID string) {
	t.Helper()
	if result.APIVersion != APIVersion {
		t.Fatalf("apiVersion = %q, want %q", result.APIVersion, APIVersion)
	}
	if result.RequestID != requestID {
		t.Fatalf("requestId = %q, want %q", result.RequestID, requestID)
	}
	for _, observation := range result.Observations {
		if observation.Label == "" || observation.Kind == "" {
			t.Fatalf("observation missing kind or label: %#v", observation)
		}
		if observation.Confidence < 0 || observation.Confidence > 1 {
			t.Fatalf("confidence out of range: %#v", observation)
		}
		if observation.Region.Width < 0 || observation.Region.Height < 0 {
			t.Fatalf("negative region dimensions: %#v", observation)
		}
		if observation.Mask != "" && !isPNG(observation.Mask) {
			t.Fatalf("mask is not a PNG: %#v", observation)
		}
	}
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func isPNG(encoded string) bool {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	return bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n"))
}
