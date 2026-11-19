package generation

import (
	"bytes"
	"context"
	"cymonkey/src/console"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"sync"

	"github.com/webong/ext/res/int/inference"
)

func (c *Client) Transcribe(ctx context.Context, request inference.TranscriptionRequest) (inference.TranscriptionResponse, error) {
	if c.config.Protocol != OpenAI {
		return inference.TranscriptionResponse{}, unsupported("transcription endpoint")
	}
	if request.Validate() != nil || len(request.Audio) > 25<<20 {
		return inference.TranscriptionResponse{}, errors.New("transcription needs a model and at most 25 MiB of audio")
	}
	options, err := console.DecodeTranscriptionOptions(request.Options)
	if err != nil {
		return inference.TranscriptionResponse{}, err
	}
	extensions := map[string]string{"audio/wav": "wav", "audio/x-wav": "wav", "audio/mpeg": "mp3", "audio/mp4": "m4a", "audio/ogg": "ogg", "audio/webm": "webm", "audio/flac": "flac"}
	extension, ok := extensions[request.MediaType]
	if !ok {
		return inference.TranscriptionResponse{}, unsupported("transcription audio format")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	responseFormat := "json"
	if options.Metadata || len(request.Timestamps) > 0 {
		responseFormat = "verbose_json"
	}
	for key, value := range map[string]string{"model": request.Model, "response_format": responseFormat, "language": request.Language, "prompt": request.Prompt} {
		if value != "" {
			if err := writer.WriteField(key, value); err != nil {
				return inference.TranscriptionResponse{}, err
			}
		}
	}
	for _, granularity := range request.Timestamps {
		if err := writer.WriteField("timestamp_granularities[]", granularity); err != nil {
			return inference.TranscriptionResponse{}, err
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="audio.`+extension+`"`)
	header.Set("Content-Type", request.MediaType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return inference.TranscriptionResponse{}, err
	}
	if _, err = part.Write(request.Audio); err != nil {
		return inference.TranscriptionResponse{}, err
	}
	if err = writer.Close(); err != nil {
		return inference.TranscriptionResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/audio/transcriptions", writer.FormDataContentType(), &body, false)
	if err != nil {
		return inference.TranscriptionResponse{}, err
	}
	defer resp.Body.Close()
	data, err := readBounded(resp.Body, c.config.MaxResponseBytes)
	if err != nil {
		return inference.TranscriptionResponse{}, err
	}
	var result struct {
		Text     *string                          `json:"text"`
		Language string                           `json:"language"`
		Duration *float64                         `json:"duration"`
		Segments []inference.TranscriptionSegment `json:"segments"`
		Words    []inference.TranscriptionWord    `json:"words"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return inference.TranscriptionResponse{}, err
	}
	if result.Text == nil {
		return inference.TranscriptionResponse{}, errors.New("transcription response has no text field")
	}
	response := inference.TranscriptionResponse{Text: *result.Text, Language: result.Language, DurationSeconds: result.Duration, Segments: result.Segments, Words: result.Words}
	return response, response.Validate()
}

func (c *Client) Synthesize(ctx context.Context, request inference.SpeechRequest) (inference.SpeechStream, error) {
	if c.config.Protocol != OpenAI {
		return nil, unsupported("speech endpoint")
	}
	format := request.Format
	if format == "" {
		format = "mp3"
	}
	_, ok := console.SpeechMediaType(format)
	if !ok || request.Model == "" || request.Input == "" || request.Voice == "" {
		return nil, errors.New("speech needs model, input, voice, and a supported format")
	}
	options, err := console.DecodeSpeechOptions(request.Options)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{"model": request.Model, "input": request.Input, "voice": request.Voice, "response_format": format}
	if options.Instructions != "" {
		payload["instructions"] = options.Instructions
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/audio/speech", "application/json", bytes.NewReader(body), false)
	if err != nil {
		return nil, err
	}
	return &audioReader{body: resp.Body, remaining: c.config.MaxResponseBytes}, nil
}

// audioReader is the bounded speech stream. Close is idempotent.
type audioReader struct {
	body      io.ReadCloser
	remaining int64
	once      sync.Once
	closeErr  error
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
func (r *audioReader) Close() error {
	r.once.Do(func() { r.closeErr = r.body.Close() })
	return r.closeErr
}
