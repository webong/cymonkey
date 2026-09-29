package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"blockade"
)

func TestOnnxProviderFixtureSmoke(t *testing.T) {
	if os.Getenv("BLOCKADE_SMOKE") != "1" {
		t.Skip("set BLOCKADE_SMOKE=1 with an exported ONNX model to run the provider smoke test")
	}
	modelPath := os.Getenv("BLOCKADE_ONNX_MODEL")
	runtimeLibrary := os.Getenv("BLOCKADE_ONNXRUNTIME_LIB")
	if modelPath == "" || runtimeLibrary == "" {
		t.Skip("BLOCKADE_ONNX_MODEL and BLOCKADE_ONNXRUNTIME_LIB are required")
	}
	for _, path := range []string{modelPath, runtimeLibrary} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("ONNX fixture dependency unavailable: %v", err)
		}
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repository root")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	image, err := os.ReadFile(filepath.Join(root, "lib", "blockade", "testdata", "smoke.png"))
	if err != nil {
		t.Fatal(err)
	}
	var providers []executionProviderConfig
	if raw := os.Getenv("BLOCKADE_ONNX_EXECUTION_PROVIDERS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &providers); err != nil {
			t.Fatalf("decode execution providers: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	engine, err := startOnnxEngine(engineConfig{
		id: "smoke-onnx-yolo", modelPath: modelPath, runtimeLibrary: runtimeLibrary,
		executionProviders: providers, confidence: defaultConfidenceThreshold, iou: defaultNMSThreshold,
	})
	if err != nil {
		t.Fatalf("start ONNX provider: %v", err)
	}
	defer engine.Close()
	result, err := engine.Observe(ctx, blockade.ObserveRequest{RequestID: "onnx-smoke-1", Image: image})
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if result.RequestID != "onnx-smoke-1" {
		t.Fatalf("requestId = %q", result.RequestID)
	}
	if err := blockade.ValidateObserveResponse(result); err != nil {
		t.Fatalf("invalid response: %v", err)
	}
}
