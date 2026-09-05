package blockade

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	defaultOnnxInputSize       = int64(640)
	defaultConfidenceThreshold = 0.25
	defaultNMSThreshold        = 0.45
)

var cocoClassNames = []string{
	"person", "bicycle", "car", "motorcycle", "airplane", "bus", "train", "truck",
	"boat", "traffic light", "fire hydrant", "stop sign", "parking meter", "bench",
	"bird", "cat", "dog", "horse", "sheep", "cow", "elephant", "bear", "zebra",
	"giraffe", "backpack", "umbrella", "handbag", "tie", "suitcase", "frisbee",
	"skis", "snowboard", "sports ball", "kite", "baseball bat", "baseball glove",
	"skateboard", "surfboard", "tennis racket", "bottle", "wine glass", "cup",
	"fork", "knife", "spoon", "bowl", "banana", "apple", "sandwich", "orange",
	"broccoli", "carrot", "hot dog", "pizza", "donut", "cake", "chair", "couch",
	"potted plant", "bed", "dining table", "toilet", "tv", "laptop", "mouse",
	"remote", "keyboard", "cell phone", "microwave", "oven", "toaster", "sink",
	"refrigerator", "book", "clock", "vase", "scissors", "teddy bear",
	"hair drier", "toothbrush",
}

var onnxRuntimeSetup struct {
	once sync.Once
	err  error
}

func initializeOnnxRuntime() error {
	onnxRuntimeSetup.once.Do(func() {
		if lib := os.Getenv("BLOCKADE_ONNXRUNTIME_LIB"); lib != "" {
			ort.SetSharedLibraryPath(lib)
		}
		onnxRuntimeSetup.err = ort.InitializeEnvironment()
	})
	return onnxRuntimeSetup.err
}

// OnnxEngine runs a YOLO detection model natively through ONNX Runtime and
// maps raw tensor output onto normalized observations.
type OnnxEngine struct {
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

// StartOnnxEngine initializes ONNX Runtime once per process and opens a
// native session for the configured YOLO detection model.
func StartOnnxEngine(e EngineConfig) (*OnnxEngine, error) {
	if e.Kind != "onnx" {
		return nil, fmt.Errorf("engine %q is not an ONNX engine", e.ID)
	}
	if e.YOLOModel == "" {
		return nil, fmt.Errorf("Blockade ONNX engine %q requires yoloModel", e.ID)
	}
	if e.SAMModel != "" {
		return nil, fmt.Errorf("Blockade ONNX engine %q does not support samModel yet; segmentation stays with local workers for now", e.ID)
	}
	if _, err := os.Stat(e.YOLOModel); err != nil {
		return nil, fmt.Errorf("Blockade ONNX engine %q yoloModel %q: %w", e.ID, e.YOLOModel, err)
	}
	if err := initializeOnnxRuntime(); err != nil {
		return nil, fmt.Errorf("initialize ONNX Runtime: %w", err)
	}
	inputs, outputs, err := ort.GetInputOutputInfo(e.YOLOModel)
	if err != nil {
		return nil, fmt.Errorf("inspect ONNX model %q: %w", e.YOLOModel, err)
	}
	if len(inputs) != 1 || len(outputs) != 1 {
		return nil, fmt.Errorf("Blockade ONNX engine %q expects one input and one output, got %d/%d", e.ID, len(inputs), len(outputs))
	}
	providerOptions, providers, err := configureOnnxExecutionProviders(e.ID, e.ExecutionProviders)
	if err != nil {
		return nil, err
	}
	engine := &OnnxEngine{
		id:         e.ID,
		inputName:  inputs[0].Name,
		outputName: outputs[0].Name,
		confidence: confidenceThreshold(),
		iou:        defaultNMSThreshold,
		evidence:   fmt.Sprintf("onnx:%s;ep=%s", filepath.Base(e.YOLOModel), strings.Join(providers, ",")),
	}
	if err := engine.resolveLayout(inputs[0], outputs[0]); err != nil {
		if providerOptions != nil {
			_ = providerOptions.Destroy()
		}
		return nil, fmt.Errorf("Blockade ONNX engine %q: %w", e.ID, err)
	}
	session, err := ort.NewDynamicAdvancedSession(e.YOLOModel, []string{engine.inputName}, []string{engine.outputName}, providerOptions)
	if providerOptions != nil {
		if destroyErr := providerOptions.Destroy(); err == nil && destroyErr != nil {
			if session != nil {
				_ = session.Destroy()
			}
			return nil, fmt.Errorf("release ONNX Runtime session options for engine %q: %w", e.ID, destroyErr)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("create ONNX session for engine %q: %w", e.ID, err)
	}
	engine.session = session
	return engine, nil
}

func (e *OnnxEngine) resolveLayout(input, output ort.InputOutputInfo) error {
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

func (e *OnnxEngine) Observe(ctx context.Context, request ObserveRequest) (ObserveResponse, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.session == nil {
		return ObserveResponse{}, fmt.Errorf("Blockade ONNX engine %q is closed", e.id)
	}
	if request.RequestID == "" {
		request.RequestID = fmt.Sprintf("blockade-onnx-%d", time.Now().UnixNano())
	}
	frame, _, err := image.Decode(bytes.NewReader(request.Image))
	if err != nil {
		return ObserveResponse{}, fmt.Errorf("decode Blockade observation image: %w", err)
	}
	bounds := frame.Bounds()
	tensorData, letterbox := rasterize(frame, int(e.inputWidth), int(e.inputHeight))
	inputTensor, err := ort.NewTensor(ort.NewShape(1, 3, e.inputHeight, e.inputWidth), tensorData)
	if err != nil {
		return ObserveResponse{}, fmt.Errorf("build ONNX input tensor: %w", err)
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
		return ObserveResponse{}, fmt.Errorf("allocate ONNX output tensor: %w", err)
	}
	defer outputTensor.Destroy()
	if ctx.Err() != nil {
		return ObserveResponse{}, ctx.Err()
	}
	if err := e.session.Run([]ort.Value{inputTensor}, []ort.Value{outputTensor}); err != nil {
		return ObserveResponse{}, fmt.Errorf("run ONNX inference: %w", err)
	}
	observations := observationsFromDetections(outputTensor.GetData(), e.outputLayout, e.numClasses, letterbox, bounds, e.confidence, e.iou, e.evidence)
	return ObserveResponse{APIVersion: APIVersion, RequestID: request.RequestID, Observations: observations}, nil
}

func (e *OnnxEngine) Capabilities(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.session == nil {
		return nil, fmt.Errorf("Blockade ONNX engine %q is closed", e.id)
	}
	return []string{CapabilityImageObserve, CapabilityObjectDetect}, nil
}

func (e *OnnxEngine) Health(ctx context.Context) (EngineHealth, error) {
	if err := ctx.Err(); err != nil {
		return EngineHealth{}, err
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return EngineHealth{Ready: e.session != nil}, nil
}

func (e *OnnxEngine) Close() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.session == nil {
		return nil
	}
	err := e.session.Destroy()
	e.session = nil
	return err
}

func confidenceThreshold() float64 {
	value, err := strconv.ParseFloat(os.Getenv("BLOCKADE_CONFIDENCE_THRESHOLD"), 64)
	if err != nil || value < 0 || value > 1 {
		return defaultConfidenceThreshold
	}
	return value
}
