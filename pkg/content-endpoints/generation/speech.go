package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"

	"jangolova/generationkit"
)

func (c *Client) Transcribe(ctx context.Context, request generationkit.TranscriptionRequest) (generationkit.TranscriptionResponse, error) {
	if c.config.Protocol != OpenAI {
		return generationkit.TranscriptionResponse{}, unsupported("transcription endpoint")
	}
	if generationkit.ValidateTranscriptionRequest(request) != nil || len(request.Audio) > 25<<20 {
		return generationkit.TranscriptionResponse{}, errors.New("transcription needs a model and at most 25 MiB of audio")
	}
	extensions := map[string]string{"audio/wav": "wav", "audio/x-wav": "wav", "audio/mpeg": "mp3", "audio/mp4": "m4a", "audio/ogg": "ogg", "audio/webm": "webm", "audio/flac": "flac"}
	extension, ok := extensions[request.MediaType]
	if !ok {
		return generationkit.TranscriptionResponse{}, unsupported("transcription audio format")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	responseFormat := "json"
	if request.Metadata || len(request.Timestamps) > 0 {
		responseFormat = "verbose_json"
	}
	for key, value := range map[string]string{"model": request.Model, "response_format": responseFormat, "language": request.Language, "prompt": request.Prompt} {
		if value != "" {
			if err := writer.WriteField(key, value); err != nil {
				return generationkit.TranscriptionResponse{}, err
			}
		}
	}
	for _, granularity := range request.Timestamps {
		if err := writer.WriteField("timestamp_granularities[]", granularity); err != nil {
			return generationkit.TranscriptionResponse{}, err
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="audio.`+extension+`"`)
	header.Set("Content-Type", request.MediaType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return generationkit.TranscriptionResponse{}, err
	}
	if _, err = part.Write(request.Audio); err != nil {
		return generationkit.TranscriptionResponse{}, err
	}
	if err = writer.Close(); err != nil {
		return generationkit.TranscriptionResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/audio/transcriptions", writer.FormDataContentType(), &body, false)
	if err != nil {
		return generationkit.TranscriptionResponse{}, err
	}
	defer resp.Body.Close()
	data, err := readBounded(resp.Body, c.config.MaxResponseBytes)
	if err != nil {
		return generationkit.TranscriptionResponse{}, err
	}
	var result struct {
		Text     *string                              `json:"text"`
		Language string                               `json:"language"`
		Duration *float64                             `json:"duration"`
		Segments []generationkit.TranscriptionSegment `json:"segments"`
		Words    []generationkit.TranscriptionWord    `json:"words"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return generationkit.TranscriptionResponse{}, err
	}
	if result.Text == nil {
		return generationkit.TranscriptionResponse{}, errors.New("transcription response has no text field")
	}
	response := generationkit.TranscriptionResponse{Text: *result.Text, Language: result.Language, DurationSeconds: result.Duration, Segments: result.Segments, Words: result.Words}
	return response, generationkit.ValidateTranscriptionResponse(response)
}

func (c *Client) Synthesize(ctx context.Context, request generationkit.SpeechRequest) (generationkit.SpeechStream, error) {
	if c.config.Protocol != OpenAI {
		return generationkit.SpeechStream{}, unsupported("speech endpoint")
	}
	mediaTypes := map[string]string{"mp3": "audio/mpeg", "opus": "audio/ogg", "aac": "audio/aac", "flac": "audio/flac", "wav": "audio/wav", "pcm": "audio/pcm"}
	format := request.Format
	if format == "" {
		format = "mp3"
	}
	mediaType, ok := mediaTypes[format]
	if !ok || request.Model == "" || request.Text == "" || request.Voice == "" {
		return generationkit.SpeechStream{}, errors.New("speech needs model, text, voice, and a supported format")
	}
	payload := map[string]any{"model": request.Model, "input": request.Text, "voice": request.Voice, "response_format": format}
	if request.Instructions != "" {
		payload["instructions"] = request.Instructions
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return generationkit.SpeechStream{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/audio/speech", "application/json", bytes.NewReader(body), false)
	if err != nil {
		return generationkit.SpeechStream{}, err
	}
	return generationkit.SpeechStream{Audio: &audioReader{body: resp.Body, remaining: c.config.MaxResponseBytes}, MediaType: mediaType}, nil
}

type audioReader struct {
	body      io.ReadCloser
	remaining int64
}

func (r *audioReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.body.Read(probe[:])
		if n > 0 {
			return 0, errors.New("speech response exceeds configured byte limit")
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.body.Read(p)
	r.remaining -= int64(n)
	return n, err
}
func (r *audioReader) Close() error { return r.body.Close() }
