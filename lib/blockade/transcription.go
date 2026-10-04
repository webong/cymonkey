package blockade

import (
	"errors"
	"math"
	"mime"
	"strings"
)

func ValidateTranscriptionRequest(r TranscriptionRequest) error {
	media, _, err := mime.ParseMediaType(r.MediaType)
	if err != nil || !strings.HasPrefix(media, "audio/") || r.Model == "" || len(r.Audio) == 0 || len(r.Audio) > 64<<20 || len(r.Prompt) > 1<<20 || len(r.Language) > 128 || len(r.Timestamps) > 2 {
		return errors.New("invalid transcription request")
	}
	seen := map[string]bool{}
	for _, kind := range r.Timestamps {
		if kind != "word" && kind != "segment" || seen[kind] {
			return errors.New("invalid transcription timestamp selection")
		}
		seen[kind] = true
	}
	return nil
}

func ValidateTranscriptionResponse(r TranscriptionResponse) error {
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
	if len(r.Text) > 32<<20 || len(r.Language) > 128 || len(r.Segments) > 100000 || len(r.Words) > 100000 || r.DurationSeconds != nil && !finite(*r.DurationSeconds) {
		return errors.New("invalid transcription metadata")
	}
	var bytes int
	for _, s := range r.Segments {
		bytes += len(s.Text)
		if s.ID < 0 || !finite(s.Start) || !finite(s.End) || s.End < s.Start {
			return errors.New("invalid transcription segment timing")
		}
	}
	for _, w := range r.Words {
		bytes += len(w.Word)
		if !finite(w.Start) || !finite(w.End) || w.End < w.Start {
			return errors.New("invalid transcription word timing")
		}
	}
	if bytes > 32<<20 {
		return errors.New("transcription metadata exceeds byte ceiling")
	}
	return nil
}
