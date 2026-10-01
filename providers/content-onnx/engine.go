package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"blockade"
	ort "github.com/yalue/onnxruntime_go"
)

const defaultOnnxInputSize = int64(640)

var onnxRuntimeSetup struct {
	once sync.Once
	err  error
}

func initializeOnnxRuntime(library string) error {
	onnxRuntimeSetup.once.Do(func() {
		if library != "" {
			ort.SetSharedLibraryPath(library)
		}
		onnxRuntimeSetup.err = ort.InitializeEnvironment()
	})
	return onnxRuntimeSetup.err
}

type engineConfig struct {
	id                 string
	modelPath          string
	runtimeLibrary     string
	executionProviders []executionProviderConfig
	confidence         float64
	iou                float64
}

func engineConfigFrom(id string, model blockade.InstalledModel, providerName string, settings map[string]string) (engineConfig, error) {
	if strings.TrimSpace(id) == "" {
		return engineConfig{}, fmt.Errorf("provider instance id is required")
	}
	if err := inspectInstalledModel(model, providerName, settings, false); err != nil {
		return engineConfig{}, err
	}
	modelPath, _ := modelArtifactPath(model, "yolo", ".onnx")
	parsed, err := parseProviderSettings(settings)
	if err != nil {
		return engineConfig{}, err
	}
	return engineConfig{
		id: id, modelPath: modelPath, runtimeLibrary: parsed.runtimeLibrary,
		executionProviders: parsed.executionProviders,
		confidence:         parsed.confidence, iou: parsed.iou,
	}, nil
}

type onnxEngine struct {
	id      string
	session *ort.DynamicAdvancedSession
	mutex   sync.Mutex

	inputName    string
	outputName   string
	inputWidth   int64
	inputHeight  int64
	candidateCap int64
	outputLayout outputLayout
	numClasses   int

	confidence float64
	iou        float64
	evidence   string
}

type outputLayout int

const (
	layoutChannelsFirst outputLayout = iota
	layoutCandidatesFirst
)

func inspectOnnxModel(path string) error {
	inputs, outputs, err := ort.GetInputOutputInfo(path)
	if err != nil {
		return fmt.Errorf("inspect ONNX model %q: %w", path, err)
	}
	if len(inputs) != 1 || len(outputs) != 1 {
		return fmt.Errorf("ONNX provider expects one input and one output, got %d/%d", len(inputs), len(outputs))
	}
	probe := &onnxEngine{}
	if err := probe.resolveLayout(inputs[0], outputs[0]); err != nil {
		return fmt.Errorf("inspect ONNX model layout: %w", err)
	}
	return nil
}

func startOnnxEngine(config engineConfig) (*onnxEngine, error) {
	if err := initializeOnnxRuntime(config.runtimeLibrary); err != nil {
		return nil, fmt.Errorf("initialize ONNX Runtime: %w", err)
	}
	inputs, outputs, err := ort.GetInputOutputInfo(config.modelPath)
	if err != nil {
		return nil, fmt.Errorf("inspect ONNX model %q: %w", config.modelPath, err)
	}
	if len(inputs) != 1 || len(outputs) != 1 {
		return nil, fmt.Errorf("ONNX provider %q expects one input and one output, got %d/%d", config.id, len(inputs), len(outputs))
	}
	providerOptions, providers, err := configureOnnxExecutionProviders(config.id, config.executionProviders)
	if err != nil {
		return nil, err
	}
	engine := &onnxEngine{
		id:         config.id,
		inputName:  inputs[0].Name,
		outputName: outputs[0].Name,
		confidence: config.confidence,
		iou:        config.iou,
		evidence:   fmt.Sprintf("onnx:%s;ep=%s", filepath.Base(config.modelPath), strings.Join(providers, ",")),
	}
	if err := engine.resolveLayout(inputs[0], outputs[0]); err != nil {
		if providerOptions != nil {
			_ = providerOptions.Destroy()
		}
		return nil, fmt.Errorf("ONNX provider %q: %w", config.id, err)
	}
	session, err := ort.NewDynamicAdvancedSession(config.modelPath, []string{engine.inputName}, []string{engine.outputName}, providerOptions)
	if providerOptions != nil {
		if destroyErr := providerOptions.Destroy(); err == nil && destroyErr != nil {
			if session != nil {
				_ = session.Destroy()
			}
			return nil, fmt.Errorf("release ONNX Runtime session options for provider %q: %w", config.id, destroyErr)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("create ONNX session for provider %q: %w", config.id, err)
	}
	engine.session = session
	return engine, nil
}

func (e *onnxEngine) resolveLayout(input, output ort.InputOutputInfo) error {
	if input.OrtValueType != ort.ONNXTypeTensor || output.OrtValueType != ort.ONNXTypeTensor {
		return fmt.Errorf("input and output must be tensors")
	}
	dims := input.Dimensions
	if len(dims) != 4 {
		return fmt.Errorf("expected 4D image input, got shape %v", dims)
	}
	height, width := dims[2], dims[3]
	if height <= 0 {
		height = defaultOnnxInputSize
	}
	if width <= 0 {
		width = defaultOnnxInputSize
	}
	e.inputHeight, e.inputWidth = height, width
	out := output.Dimensions
	if len(out) != 3 {
		return fmt.Errorf("unsupported detection output shape %v", out)
	}
	channelAxis := out[1]
	candidateAxis := out[2]
	e.outputLayout = layoutChannelsFirst
	if out[2] < out[1] {
		channelAxis, candidateAxis = out[2], out[1]
		e.outputLayout = layoutCandidatesFirst
	}
	e.numClasses = int(channelAxis) - 4
	e.candidateCap = candidateAxis
	if e.numClasses <= 0 || e.candidateCap <= 0 {
		return fmt.Errorf("detection output shape %v has no usable candidates", out)
	}
	return nil
}

func (e *onnxEngine) Observe(ctx context.Context, request blockade.ObserveRequest) (blockade.ObserveResponse, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.session == nil {
		return blockade.ObserveResponse{}, fmt.Errorf("ONNX provider %q is closed", e.id)
	}
	if request.RequestID == "" {
		request.RequestID = fmt.Sprintf("content-onnx-%d", time.Now().UnixNano())
	}
	frame, _, err := image.Decode(bytes.NewReader(request.Image))
	if err != nil {
		return blockade.ObserveResponse{}, fmt.Errorf("decode observation image: %w", err)
	}
	bounds := frame.Bounds()
	tensorData, letterbox := rasterize(frame, int(e.inputWidth), int(e.inputHeight))
	inputTensor, err := ort.NewTensor(ort.NewShape(1, 3, e.inputHeight, e.inputWidth), tensorData)
	if err != nil {
		return blockade.ObserveResponse{}, fmt.Errorf("build ONNX input tensor: %w", err)
	}
	defer inputTensor.Destroy()
	channels := int64(e.numClasses + 4)
	var outShape ort.Shape
	if e.outputLayout == layoutChannelsFirst {
		outShape = ort.NewShape(1, channels, e.candidateCap)
	} else {
		outShape = ort.NewShape(1, e.candidateCap, channels)
	}
	outputTensor, err := ort.NewTensor(outShape, make([]float32, channels*e.candidateCap))
	if err != nil {
		return blockade.ObserveResponse{}, fmt.Errorf("allocate ONNX output tensor: %w", err)
	}
	defer outputTensor.Destroy()
	if err := ctx.Err(); err != nil {
		return blockade.ObserveResponse{}, err
	}
	if err := e.session.Run([]ort.Value{inputTensor}, []ort.Value{outputTensor}); err != nil {
		return blockade.ObserveResponse{}, fmt.Errorf("run ONNX inference: %w", err)
	}
	observations := observationsFromDetections(outputTensor.GetData(), e.outputLayout, e.numClasses, letterbox, bounds, e.confidence, e.iou, e.evidence)
	return blockade.ObserveResponse{APIVersion: blockade.APIVersion, RequestID: request.RequestID, Observations: observations}, nil
}

func (e *onnxEngine) Close() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.session == nil {
		return nil
	}
	err := e.session.Destroy()
	e.session = nil
	return err
}
