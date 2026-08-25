package blockade

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"testing"
)

func TestRasterizeLetterboxMath(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 255, G: 255, B: 255, A: 255}}, image.Point{}, draw.Src)
	data, letterbox := rasterize(img, 640, 640)
	if len(data) != 3*640*640 {
		t.Fatalf("planar size = %d", len(data))
	}
	if letterbox.scale != 2.0 {
		t.Fatalf("scale = %v, want 2.0", letterbox.scale)
	}
	if letterbox.padX != 0 || letterbox.padY != 80 {
		t.Fatalf("padding = (%v, %v), want (0, 80)", letterbox.padX, letterbox.padY)
	}
	background := color.RGBA{R: 114, G: 114, B: 114, A: 255}
	for _, point := range []image.Point{{10, 10}, {320, 5}, {630, 620}, {100, 600}} {
		if got := data[point.Y*640+point.X]; got != float32(background.R)/255 {
			t.Fatalf("pixel %v = %v, want gray padding", point, got)
		}
	}
}

func TestRasterizeSquareImageHasNoPadding(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	_, letterbox := rasterize(img, 64, 64)
	if letterbox.scale != 1 || letterbox.padX != 0 || letterbox.padY != 0 {
		t.Fatalf("letterbox = %#v", letterbox)
	}
}

func TestObservationsFromDetectionsChannelsFirst(t *testing.T) {
	const candidates = 3
	const classes = 2
	data := make([]float32, (classes+4)*candidates)
	data[0] = 50
	data[candidates] = 40
	data[2*candidates] = 20
	data[3*candidates] = 10
	data[(4+1)*candidates] = 0.9
	bounds := image.Rect(0, 0, 100, 100)
	result := observationsFromDetections(data, layoutChannelsFirst, classes, letterboxScale{scale: 1}, bounds, 0.25, 0.45, "test")
	if len(result) != 1 {
		t.Fatalf("observations = %#v", result)
	}
	observation := result[0]
	if observation.Kind != "object" || observation.Label != cocoClassNames[1] || math.Abs(observation.Confidence-0.9) > 1e-6 {
		t.Fatalf("observation = %#v", observation)
	}
	if observation.Region.X != 40 || observation.Region.Y != 35 || observation.Region.Width != 20 || observation.Region.Height != 10 {
		t.Fatalf("region = %#v", observation.Region)
	}
	if observation.Evidence != "test" {
		t.Fatalf("evidence = %q", observation.Evidence)
	}
}

func TestObservationsFromDetectionsCandidatesFirstAndNMS(t *testing.T) {
	const candidates = 2
	const classes = 1
	channels := classes + 4
	data := make([]float32, channels*candidates)
	boxA := []float32{50, 50, 30, 30, 0.8}
	boxB := []float32{51, 51, 30, 30, 0.7}
	copy(data[0:channels], boxA)
	copy(data[channels:2*channels], boxB)
	bounds := image.Rect(0, 0, 200, 200)
	result := observationsFromDetections(data, layoutCandidatesFirst, classes, letterboxScale{scale: 1}, bounds, 0.25, 0.45, "nms")
	if len(result) != 1 || math.Abs(float64(result[0].Confidence)-0.8) > 1e-6 {
		t.Fatalf("overlapping boxes not suppressed: %#v", result)
	}
}

func TestObservationsFromDetectionsUnletterboxesCoordinates(t *testing.T) {
	const candidates = 1
	const classes = 1
	channels := classes + 4
	data := make([]float32, channels*candidates)
	copy(data[0:channels], []float32{340, 400, 40, 20, 0.5})
	bounds := image.Rect(0, 0, 320, 240)
	result := observationsFromDetections(data, layoutChannelsFirst, classes, letterboxScale{scale: 2, padX: 0, padY: 80}, bounds, 0.25, 0.45, "lb")
	if len(result) != 1 {
		t.Fatalf("observations = %#v", result)
	}
	region := result[0].Region
	if region.X != 160 || region.Y != 155 || region.Width != 20 || region.Height != 10 {
		t.Fatalf("region = %#v, want x=160 y=155 w=20 h=10", region)
	}
}

func TestObservationsFromDetectionsKeepsSuppressedIndicesCorrectly(t *testing.T) {
	const classes = 1
	channels := classes + 4
	data := make([]float32, channels*3)
	copy(data[0:channels], []float32{5, 5, 10, 10, 0.9})
	copy(data[channels:2*channels], []float32{5, 5, 8, 8, 0.8})
	copy(data[2*channels:3*channels], []float32{55, 55, 10, 10, 0.7})
	bounds := image.Rect(0, 0, 100, 100)
	result := observationsFromDetections(data, layoutCandidatesFirst, classes, letterboxScale{scale: 1}, bounds, 0.25, 0.45, "regression")
	if len(result) != 2 {
		t.Fatalf("observations = %#v", result)
	}
	if math.Abs(result[1].Confidence-0.7) > 1e-6 {
		t.Fatalf("second observation came from a suppressed detection: %#v", result)
	}
}

func TestIntersectionOverUnion(t *testing.T) {
	identical := intersectionOverUnion([4]float64{0, 0, 10, 10}, [4]float64{0, 0, 10, 10})
	if identical != 1 {
		t.Fatalf("identical iou = %v", identical)
	}
	disjoint := intersectionOverUnion([4]float64{0, 0, 10, 10}, [4]float64{20, 20, 30, 30})
	if disjoint != 0 {
		t.Fatalf("disjoint iou = %v", disjoint)
	}
	half := intersectionOverUnion([4]float64{0, 0, 10, 10}, [4]float64{0, 0, 10, 20})
	if half != 0.5 {
		t.Fatalf("half overlap iou = %v", half)
	}
}

func TestConfidenceThresholdEnvironmentOverride(t *testing.T) {
	t.Setenv("BLOCKADE_CONFIDENCE_THRESHOLD", "0.75")
	if got := confidenceThreshold(); got != 0.75 {
		t.Fatalf("threshold = %v", got)
	}
	t.Setenv("BLOCKADE_CONFIDENCE_THRESHOLD", "not-a-number")
	if got := confidenceThreshold(); got != defaultConfidenceThreshold {
		t.Fatalf("invalid threshold = %v", got)
	}
	t.Setenv("BLOCKADE_CONFIDENCE_THRESHOLD", "1.5")
	if got := confidenceThreshold(); got != defaultConfidenceThreshold {
		t.Fatalf("out-of-range threshold = %v", got)
	}
}

func TestClassNameFallback(t *testing.T) {
	if className(0) != "person" || className(len(cocoClassNames)-1) == "" {
		t.Fatal("unexpected class names")
	}
	if className(-1) != "-1" || className(len(cocoClassNames)) == "" {
		t.Fatalf("out-of-range names = %q/%q", className(-1), className(len(cocoClassNames)))
	}
}

func TestOnnxConfigValidationRequiresModelFile(t *testing.T) {
	config := Config{
		APIVersion: ConfigAPIVersion,
		Engines: []EngineConfig{{
			ID:        "onnx-yolo",
			Kind:      "onnx",
			YOLOModel: "missing-model.onnx",
		}},
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := config.ValidateModelFiles(); err == nil {
		t.Fatal("expected missing ONNX model to fail file validation")
	}
}

func TestPNGFixtureDecodesForOnnxEngine(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil || format != "png" {
		t.Fatalf("decode: %v format=%s", err, format)
	}
	if decoded.Bounds().Dx() != 8 {
		t.Fatalf("bounds = %#v", decoded.Bounds())
	}
}
