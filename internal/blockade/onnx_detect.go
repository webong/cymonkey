package blockade

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"strconv"
)

type letterboxScale struct {
	scale float64
	padX  float64
	padY  float64
}

// rasterize letterboxes an image into a fixed square input with gray padding,
// returning normalized NCHW planar pixels ready for a YOLO ONNX model.
func rasterize(img image.Image, targetW, targetH int) ([]float32, letterboxScale) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	scale := math.Min(float64(targetW)/float64(width), float64(targetH)/float64(height))
	resizedW := int(math.Round(float64(width) * scale))
	resizedH := int(math.Round(float64(height) * scale))
	if resizedW < 1 {
		resizedW = 1
	}
	if resizedH < 1 {
		resizedH = 1
	}
	padX := float64(targetW-resizedW) / 2
	padY := float64(targetH-resizedH) / 2

	scaled := image.NewRGBA(image.Rect(0, 0, resizedW, resizedH))
	for y := 0; y < resizedH; y++ {
		sourceY := bounds.Min.Y + int(math.Min(float64(y)/scale, float64(height-1)))
		for x := 0; x < resizedW; x++ {
			sourceX := bounds.Min.X + int(math.Min(float64(x)/scale, float64(width-1)))
			scaled.Set(x, y, img.At(sourceX, sourceY))
		}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{R: 114, G: 114, B: 114, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(int(padX), int(padY), int(padX)+resizedW, int(padY)+resizedH), scaled, image.Point{}, draw.Src)

	plane := targetW * targetH
	data := make([]float32, 3*plane)
	for y := 0; y < targetH; y++ {
		for x := 0; x < targetW; x++ {
			r, g, b, _ := canvas.At(x, y).RGBA()
			index := y*targetW + x
			data[index] = float32(r>>8) / 255
			data[plane+index] = float32(g>>8) / 255
			data[2*plane+index] = float32(b>>8) / 255
		}
	}
	return data, letterboxScale{scale: scale, padX: padX, padY: padY}
}

type rawDetection struct {
	xyxy       [4]float64
	score      float64
	classIndex int
}

// observationsFromDetections maps raw YOLO ONNX tensor output onto normalized
// observations, undoing the letterbox transform and applying threshold plus NMS.
func observationsFromDetections(
	data []float32,
	layout outputLayout,
	numClasses int,
	letterbox letterboxScale,
	bounds image.Rectangle,
	confidenceThreshold float64,
	iouThreshold float64,
	evidence string,
) []Observation {
	channels := numClasses + 4
	candidates := len(data) / channels
	detections := make([]rawDetection, 0, candidates)
	for j := 0; j < candidates; j++ {
		var centerX, centerY, boxWidth, boxHeight float64
		bestClass, bestScore := -1, 0.0
		switch layout {
		case layoutChannelsFirst:
			centerX = float64(data[j])
			centerY = float64(data[candidates+j])
			boxWidth = float64(data[2*candidates+j])
			boxHeight = float64(data[3*candidates+j])
			for c := 0; c < numClasses; c++ {
				score := float64(data[(4+c)*candidates+j])
				if score > bestScore {
					bestClass, bestScore = c, score
				}
			}
		case layoutCandidatesFirst:
			base := j * channels
			centerX = float64(data[base])
			centerY = float64(data[base+1])
			boxWidth = float64(data[base+2])
			boxHeight = float64(data[base+3])
			for c := 0; c < numClasses; c++ {
				score := float64(data[base+4+c])
				if score > bestScore {
					bestClass, bestScore = c, score
				}
			}
		default:
			return nil
		}
		if bestClass < 0 || bestScore < confidenceThreshold {
			continue
		}
		detections = append(detections, rawDetection{
			xyxy: [4]float64{
				centerX - boxWidth/2,
				centerY - boxHeight/2,
				centerX + boxWidth/2,
				centerY + boxHeight/2,
			},
			score:      bestScore,
			classIndex: bestClass,
		})
	}
	sort.SliceStable(detections, func(i, j int) bool {
		return detections[i].score > detections[j].score
	})
	kept := nonMaxSuppression(detections, iouThreshold)
	observations := make([]Observation, 0, len(kept))
	for _, index := range kept {
		box := detections[index]
		observations = append(observations, Observation{
			Kind:       "object",
			Label:      className(box.classIndex),
			Confidence: clamp01(box.score),
			Region:     regionFromLetterboxed(box.xyxy, letterbox, bounds),
			Evidence:   evidence,
		})
	}
	return observations
}

func nonMaxSuppression(detections []rawDetection, iouThreshold float64) []int {
	suppressed := make([]bool, len(detections))
	var keep []int
	for i := range detections {
		if suppressed[i] {
			continue
		}
		keep = append(keep, i)
		for j := i + 1; j < len(detections); j++ {
			if !suppressed[j] && intersectionOverUnion(detections[i].xyxy, detections[j].xyxy) > iouThreshold {
				suppressed[j] = true
			}
		}
	}
	return keep
}

func intersectionOverUnion(a, b [4]float64) float64 {
	intersectX1 := math.Max(a[0], b[0])
	intersectY1 := math.Max(a[1], b[1])
	intersectX2 := math.Min(a[2], b[2])
	intersectY2 := math.Min(a[3], b[3])
	intersectWidth := math.Max(intersectX2-intersectX1, 0)
	intersectHeight := math.Max(intersectY2-intersectY1, 0)
	intersection := intersectWidth * intersectHeight
	areaA := (a[2] - a[0]) * (a[3] - a[1])
	areaB := (b[2] - b[0]) * (b[3] - b[1])
	union := areaA + areaB - intersection
	if union <= 0 {
		return 0
	}
	return intersection / union
}

func regionFromLetterboxed(xyxy [4]float64, letterbox letterboxScale, bounds image.Rectangle) Region {
	width, height := float64(bounds.Dx()), float64(bounds.Dy())
	x1 := clampFloat((xyxy[0]-letterbox.padX)/letterbox.scale, 0, width)
	y1 := clampFloat((xyxy[1]-letterbox.padY)/letterbox.scale, 0, height)
	x2 := clampFloat((xyxy[2]-letterbox.padX)/letterbox.scale, 0, width)
	y2 := clampFloat((xyxy[3]-letterbox.padY)/letterbox.scale, 0, height)
	return Region{X: x1, Y: y1, Width: math.Max(x2-x1, 0), Height: math.Max(y2-y1, 0)}
}

func className(index int) string {
	if index < 0 || index >= len(cocoClassNames) {
		return strconv.Itoa(index)
	}
	return cocoClassNames[index]
}

func clampFloat(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

func clamp01(value float64) float64 {
	return clampFloat(value, 0, 1)
}
