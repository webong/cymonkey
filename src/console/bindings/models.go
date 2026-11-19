package bindings

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"strings"

	"cymonkey/src/console"
	"github.com/webong/ext/res/int/inference"
)

// GenerationInput contains the inputs a caller may choose for a bound model
// tool. Model selection and nested tool definitions belong to the host.
type GenerationInput struct {
	Messages        []inference.Message `json:"messages"`
	MaxOutputTokens int                 `json:"maxOutputTokens,omitempty"`
	Temperature     *float64            `json:"temperature,omitempty"`
	OutputSchema    json.RawMessage     `json:"outputSchema,omitempty"`
}

// Generate binds one model to one tool. It makes one generation request and
// returns the complete response, including proposed calls as data.
func Generate(definition console.Definition, generator inference.Generator, model string) (console.Tool, error) {
	if generator == nil || strings.TrimSpace(model) == "" {
		return console.Tool{}, errors.New("generation needs a generator and fixed model")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, input GenerationInput) (inference.GenerationResponse, error) {
		request := inference.GenerationRequest{
			Model: model, Messages: input.Messages, MaxOutputTokens: input.MaxOutputTokens,
			Temperature: input.Temperature, OutputSchema: input.OutputSchema,
		}
		if err := request.Validate(); err != nil {
			return inference.GenerationResponse{}, err
		}
		response, err := generator.Generate(ctx, request)
		if err != nil {
			return inference.GenerationResponse{}, err
		}
		if err := console.ValidateResult(response, request); err != nil {
			return inference.GenerationResponse{}, err
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
	Audio      []byte          `json:"audio"`
	MediaType  string          `json:"mediaType"`
	Language   string          `json:"language,omitempty"`
	Prompt     string          `json:"prompt,omitempty"`
	Timestamps []string        `json:"timestamps,omitempty"`
	Options    json.RawMessage `json:"options,omitempty"` // provider options, such as {"metadata": true}
}

func Transcribe(definition console.Definition, transcriber inference.Transcriber, options TranscriptionOptions) (console.Tool, error) {
	if transcriber == nil || strings.TrimSpace(options.Model) == "" || options.MaxAudioBytes <= 0 {
		return console.Tool{}, errors.New("transcription needs a transcriber, fixed model, and audio byte ceiling")
	}
	return console.NewJSONTool(definition, func(ctx context.Context, input TranscriptionInput) (inference.TranscriptionResponse, error) {
		mediaType, _, err := mime.ParseMediaType(input.MediaType)
		if err != nil || !strings.Contains(mediaType, "/") || len(input.Audio) == 0 || int64(len(input.Audio)) > options.MaxAudioBytes {
			return inference.TranscriptionResponse{}, errors.New("transcription needs a valid media type and audio within the host byte ceiling")
		}
		request := inference.TranscriptionRequest{
			Model: options.Model, Audio: input.Audio, MediaType: input.MediaType,
			Language: input.Language, Prompt: input.Prompt, Timestamps: input.Timestamps, Options: input.Options,
		}
		if err := request.Validate(); err != nil {
			return inference.TranscriptionResponse{}, err
		}
		result, err := transcriber.Transcribe(ctx, request)
		if err != nil {
			return result, err
		}
		return result, result.Validate()
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
	Input string `json:"input"`
}

// SpeechConsumer synchronously consumes a borrowed audio stream of the given
// media type and returns JSON metadata, such as a host-owned content reference
// or playback receipt. The binding closes the stream when the consumer
// returns. Consumers must not retain it, and own destination selection,
// authorization, and byte/time limits.
type SpeechConsumer func(ctx context.Context, stream inference.SpeechStream, mediaType string) (json.RawMessage, error)

// Synthesize binds a fixed model, voice, format, and audio consumer. Only text
// is chosen by the tool caller; device and file destinations remain host-owned.
func Synthesize(definition console.Definition, synthesizer inference.Synthesizer, options SpeechOptions, consume SpeechConsumer) (console.Tool, error) {
	if synthesizer == nil || consume == nil || strings.TrimSpace(options.Model) == "" || strings.TrimSpace(options.Voice) == "" || strings.TrimSpace(options.Format) == "" || options.MaxTextBytes <= 0 {
		return console.Tool{}, errors.New("speech needs a synthesizer, fixed model/voice/format, text byte ceiling, and audio consumer")
	}
	mediaType, ok := console.SpeechMediaType(options.Format)
	if !ok {
		return console.Tool{}, errors.New("speech format is not supported")
	}
	var providerOptions json.RawMessage
	if options.Instructions != "" {
		providerOptions, _ = json.Marshal(console.SpeechOptions{Instructions: options.Instructions})
	}
	return console.NewJSONTool(definition, func(ctx context.Context, input SpeechInput) (output json.RawMessage, err error) {
		if strings.TrimSpace(input.Input) == "" || len(input.Input) > options.MaxTextBytes {
			return nil, errors.New("speech input must be nonempty and within the host byte ceiling")
		}
		stream, err := synthesizer.Synthesize(ctx, inference.SpeechRequest{
			Model: options.Model, Input: input.Input, Voice: options.Voice,
			Format: options.Format, Options: providerOptions,
		})
		if err != nil {
			// A faulty adapter may return an attachment alongside an error.
			if stream != nil {
				err = errors.Join(err, stream.Close())
			}
			return nil, err
		}
		if stream == nil {
			return nil, errors.New("speech adapter returned no audio stream")
		}
		defer func() { err = errors.Join(err, stream.Close()) }()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return consume(ctx, stream, mediaType)
	})
}
