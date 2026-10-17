package main

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"

	ort "github.com/yalue/onnxruntime_go"
)

func TestRasterizeLetterboxMath(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 255, G: 255, B: 255, A: 255}}, image.Point{}, draw.Src)
	data, letterbox := rasterize(img, 640, 640)
	if len(data) != 3*640*640 || letterbox.scale != 2 || letterbox.padX != 0 || letterbox.padY != 80 {
		t.Fatalf("rasterized size=%d letterbox=%#v", len(data), letterbox)
	}
}

func TestObservationsFromDetectionsAndNMS(t *testing.T) {
	const classes = 1
	const channels = classes + 4
	data := make([]float32, channels*3)
	copy(data[0:channels], []float32{5, 5, 10, 10, 0.9})
	copy(data[channels:2*channels], []float32{5, 5, 8, 8, 0.8})
	copy(data[2*channels:3*channels], []float32{55, 55, 10, 10, 0.7})
	result := observationsFromDetections(data, layoutCandidatesFirst, classes, letterboxScale{scale: 1}, image.Rect(0, 0, 100, 100), 0.25, 0.45, "test")
	if len(result) != 2 || math.Abs(result[1].Confidence-0.7) > 1e-6 {
		t.Fatalf("observations = %#v", result)
	}
}

func TestIntersectionOverUnion(t *testing.T) {
	if got := intersectionOverUnion([4]float64{0, 0, 10, 10}, [4]float64{0, 0, 10, 20}); got != 0.5 {
		t.Fatalf("intersection over union = %v", got)
	}
}

func tensorInfo(dimensions ...int64) ort.InputOutputInfo {
	return ort.InputOutputInfo{OrtValueType: ort.ONNXTypeTensor, Dimensions: ort.NewShape(dimensions...)}
}

func TestResolveLayoutHandlesStaticAndDynamicCandidateAxes(t *testing.T) {
	input := tensorInfo(1, 3, 640, 640)
	cases := []struct {
		name       string
		output     []int64
		layout     outputLayout
		classes    int
		candidates int64
		wantErr    bool
	}{
		{"static channels first", []int64{1, 84, 8400}, layoutChannelsFirst, 80, 8400, false},
		{"static candidates first", []int64{1, 8400, 84}, layoutCandidatesFirst, 80, 8400, false},
		{"dynamic batch and candidates (official yolo11n export)", []int64{-1, 84, -1}, layoutChannelsFirst, 80, 0, false},
		{"dynamic candidates first", []int64{-1, -1, 84}, layoutCandidatesFirst, 80, 0, false},
		{"dynamic candidates with a fixed batch", []int64{1, 84, -1}, layoutChannelsFirst, 80, 0, false},
		{"no static channel axis", []int64{-1, -1, -1}, 0, 0, 0, true},
		{"too few channels", []int64{1, 4, 8400}, 0, 0, 0, true},
		{"not three dimensions", []int64{1, 84}, 0, 0, 0, true},
	}
	for _, c := range cases {
		engine := &onnxEngine{}
		err := engine.resolveLayout(input, tensorInfo(c.output...))
		if c.wantErr {
			if err == nil {
				t.Fatalf("%s: accepted %v", c.name, c.output)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if engine.outputLayout != c.layout || engine.numClasses != c.classes || engine.candidateCap != c.candidates {
			t.Fatalf("%s: layout=%v classes=%d candidates=%d", c.name, engine.outputLayout, engine.numClasses, engine.candidateCap)
		}
	}
}
