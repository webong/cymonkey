package bindings

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"strings"

	"console"
	"generationkit"
)

// GenerationInput contains the inputs a caller may choose for a bound model
// tool. Model selection and nested tool definitions belong to the host.
type GenerationInput struct {
	Messages        []generationkit.Message `json:"messages"`
	MaxOutputTokens int                     `json:"maxOutputTokens,omitempty"`
	Temperature     *float64                `json:"temperature,omitempty"`
	OutputSchema    json.RawMessage         `json:"outputSchema,omitempty"`
}

// Generate binds one model to one tool. It makes one generation request and
// returns the complete response, including proposed calls as data.
func Generate(definition console.Definition, generator generationkit.Generator, model string) (console.Tool, error) {
	if generator == nil || strings.TrimSpace(model) == "" {
		return console.Tool{}, errors.New("generation needs a generator and fixed model")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, input GenerationInput) (generationkit.GenerationResponse, error) {
		request := generationkit.GenerationRequest{
			Model: model, Messages: input.Messages, MaxOutputTokens: input.MaxOutputTokens,
			Temperature: input.Temperature, OutputSchema: input.OutputSchema,
		}
		if err := generationkit.ValidateGenerationRequest(request); err != nil {
			return generationkit.GenerationResponse{}, err
		}
		response, err := generator.Generate(ctx, request)
		if err != nil {
			return generationkit.GenerationResponse{}, err
		}
		if err := generationkit.ValidateGenerationResult(response, request); err != nil {
			return generationkit.GenerationResponse{}, err
		}
		return response, nil
	})
}

type TranscriptionOptions struct {
	Model         string
	MaxAudioBytes int64 // positive, explicit per-call ceiling chosen by the host
}

// Audio is base64 in the tool's JSON arguments, following encoding/json's
// standard []byte representation. No device is opened by this binding.
type TranscriptionInput struct {
	Audio      []byte   `json:"audio"`
	MediaType  string   `json:"mediaType"`
	Language   string   `json:"language,omitempty"`
	Prompt     string   `json:"prompt,omitempty"`
	Metadata   bool     `json:"metadata,omitempty"`
	Timestamps []string `json:"timestamps,omitempty"`
}

func Transcribe(definition console.Definition, transcriber generationkit.Transcriber, options TranscriptionOptions) (console.Tool, error) {
	if transcriber == nil || strings.TrimSpace(options.Model) == "" || options.MaxAudioBytes <= 0 {
		return console.Tool{}, errors.New("transcription needs a transcriber, fixed model, and audio byte ceiling")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, input TranscriptionInput) (generationkit.TranscriptionResponse, error) {
		mediaType, _, err := mime.ParseMediaType(input.MediaType)
		if err != nil || !strings.Contains(mediaType, "/") || len(input.Audio) == 0 || int64(len(input.Audio)) > options.MaxAudioBytes {
			return generationkit.TranscriptionResponse{}, errors.New("transcription needs a valid media type and audio within the host byte ceiling")
		}
		request := generationkit.TranscriptionRequest{
			Model: options.Model, Audio: input.Audio, MediaType: input.MediaType,
			Language: input.Language, Prompt: input.Prompt, Metadata: input.Metadata, Timestamps: input.Timestamps,
		}
		if err := generationkit.ValidateTranscriptionRequest(request); err != nil {
			return generationkit.TranscriptionResponse{}, err
		}
		result, err := transcriber.Transcribe(ctx, request)
		if err != nil {
			return result, err
		}
		return result, generationkit.ValidateTranscriptionResponse(result)
	})
}

type SpeechOptions struct {
	Model        string
	Voice        string
	Format       string
	Instructions string
	MaxTextBytes int // positive, explicit per-call ceiling chosen by the host
}

type SpeechInput struct {
	Text string `json:"text"`
}

// SpeechConsumer synchronously consumes a borrowed audio stream and returns
// JSON metadata, such as a host-owned content reference or playback receipt.
// The binding closes the stream when the consumer returns. Consumers must not
// retain it, and own destination selection, authorization, and byte/time limits.
type SpeechConsumer func(context.Context, generationkit.SpeechStream) (json.RawMessage, error)

// Synthesize binds a fixed model, voice, format, and audio consumer. Only text
// is chosen by the tool caller; device and file destinations remain host-owned.
func Synthesize(definition console.Definition, synthesizer generationkit.SpeechSynthesizer, options SpeechOptions, consume SpeechConsumer) (console.Tool, error) {
	if synthesizer == nil || consume == nil || strings.TrimSpace(options.Model) == "" || strings.TrimSpace(options.Voice) == "" || strings.TrimSpace(options.Format) == "" || options.MaxTextBytes <= 0 {
		return console.Tool{}, errors.New("speech needs a synthesizer, fixed model/voice/format, text byte ceiling, and audio consumer")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, input SpeechInput) (output json.RawMessage, err error) {
		if strings.TrimSpace(input.Text) == "" || len(input.Text) > options.MaxTextBytes {
			return nil, errors.New("speech text must be nonempty and within the host byte ceiling")
		}
		stream, err := synthesizer.Synthesize(ctx, generationkit.SpeechRequest{
			Model: options.Model, Text: input.Text, Voice: options.Voice,
			Format: options.Format, Instructions: options.Instructions,
		})
		if err != nil {
			// A faulty adapter may return an attachment alongside an error.
			if stream.Audio != nil {
				err = errors.Join(err, stream.Audio.Close())
			}
			return nil, err
		}
		if stream.Audio == nil {
			return nil, errors.New("speech adapter returned no audio stream")
		}
		defer func() { err = errors.Join(err, stream.Audio.Close()) }()
		mediaType, _, parseErr := mime.ParseMediaType(stream.MediaType)
		if parseErr != nil || !strings.HasPrefix(mediaType, "audio/") {
			return nil, errors.New("speech adapter returned an invalid audio media type")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return consume(ctx, stream)
	})
}
