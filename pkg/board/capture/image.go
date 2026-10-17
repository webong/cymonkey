package capture

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// Crop keeps image-local pixel coordinates explicit. It produces new bytes;
// original frame evidence and snapshot IDs are never rewritten.
func Crop(frame Frame, region Region, maxBytes, maxPixels int) (Frame, error) {
	img, err := decode(frame, maxBytes, maxPixels)
	if err != nil {
		return Frame{}, err
	}
	b := img.Bounds()
	if region.X < 0 || region.Y < 0 || region.Width < 1 || region.Height < 1 || region.X > b.Dx()-region.Width || region.Y > b.Dy()-region.Height {
		return Frame{}, errors.New("crop exceeds image bounds")
	}
	output := image.NewRGBA(image.Rect(0, 0, region.Width, region.Height))
	draw.Draw(output, output.Bounds(), img, image.Pt(b.Min.X+region.X, b.Min.Y+region.Y), draw.Src)
	var data bytes.Buffer
	if err = png.Encode(&data, output); err != nil {
		return Frame{}, err
	}
	if data.Len() > maxBytes {
		return Frame{}, errors.New("cropped frame exceeds byte ceiling")
	}
	frame.Data = data.Bytes()
	// A cropped image cannot be submitted as original-window coordinates.
	frame.CoordinateSpace = "crop-local-pixels"
	return frame, nil
}

type Annotation struct {
	ID     string     `json:"id"`
	Region Region     `json:"region"`
	Color  color.RGBA `json:"color"`
}

// Annotate draws bounded element boxes. IDs and bounds remain in the supplied
// legend; drawing never changes the source snapshot or authorizes an action.
func Annotate(frame Frame, boxes []Annotation, maxBytes, maxPixels int) ([]byte, error) {
	if len(boxes) > 500 {
		return nil, errors.New("annotation count exceeds ceiling")
	}
	img, err := decode(frame, maxBytes, maxPixels)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	output := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(output, output.Bounds(), img, b.Min, draw.Src)
	seen := map[string]bool{}
	for _, box := range boxes {
		r := box.Region
		if box.ID == "" || len(box.ID) > 256 || seen[box.ID] || r.X < 0 || r.Y < 0 || r.Width < 1 || r.Height < 1 || r.X > b.Dx()-r.Width || r.Y > b.Dy()-r.Height {
			return nil, errors.New("invalid annotation ID or region")
		}
		seen[box.ID] = true
		c := box.Color
		for x := r.X; x < r.X+r.Width; x++ {
			output.SetRGBA(x, r.Y, c)
			output.SetRGBA(x, r.Y+r.Height-1, c)
		}
		for y := r.Y; y < r.Y+r.Height; y++ {
			output.SetRGBA(r.X, y, c)
			output.SetRGBA(r.X+r.Width-1, y, c)
		}
	}
	var data bytes.Buffer
	err = png.Encode(&data, output)
	if data.Len() > maxBytes {
		return nil, errors.New("annotated image exceeds byte ceiling")
	}
	return data.Bytes(), err
}
