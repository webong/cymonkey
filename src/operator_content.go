package main

import (
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"time"

	"blockade"
	cymonkeyhost "cymonkey/src/internal/host"
)

func (server *operatorServer) blockadeEndpoint() (string, error) {
	if server.blockadeURL != "" {
		return server.blockadeURL, nil
	}
	if server.config == "" {
		return "", errors.New("start the operator with --blockade-url or --config")
	}
	config, err := cymonkeyhost.LoadConfig(server.config)
	if err != nil || config.Observation == nil || config.Observation.Blockade.Endpoint == "" {
		return "", errors.New("Blockade endpoint is unavailable in the operator configuration")
	}
	return config.Observation.Blockade.Endpoint, nil
}

func (server *operatorServer) handleInterpretation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		operatorError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	endpoint, err := server.blockadeEndpoint()
	if err != nil {
		operatorError(w, http.StatusServiceUnavailable, "interpretation_unconfigured", err.Error())
		return
	}
	var request blockade.InterpretRequest
	if err := decodeOperatorJSONLimit(w, r, &request, 16<<20); err != nil {
		operatorError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if request.APIVersion == "" {
		request.APIVersion = blockade.ContentAPIVersion
	}
	if err := blockade.ValidateInterpretRequest(request); err != nil {
		operatorError(w, http.StatusUnprocessableEntity, "invalid_content", err.Error())
		return
	}
	result, err := (blockade.Client{BaseURL: endpoint}).Interpret(r.Context(), request)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, blockade.ErrModalityUnsupported) {
			status = http.StatusNotImplemented
		}
		operatorError(w, status, "interpretation_failed", err.Error())
		return
	}
	operatorJSON(w, http.StatusOK, result)
}

func operatorInterpretClient(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("cymonkey operator interpret", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	base := flags.String("url", defaultOperatorURL, "operator server URL")
	text := flags.String("text", "", "text content")
	textFile := flags.String("text-file", "", "UTF-8 text file on this machine")
	imagePath := flags.String("image", "", "image file on this machine")
	audioPath := flags.String("audio", "", "audio file on this machine")
	audioType := flags.String("audio-type", "", "audio MIME type, such as audio/wav")
	videoPath := flags.String("video", "", "video file on this machine")
	videoType := flags.String("video-type", "", "video MIME type, such as video/mp4")
	prompt := flags.String("prompt", "", "optional interpretation prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("operator interpret accepts flags only")
	}
	parts := make([]blockade.ContentPart, 0, 4)
	if *text != "" {
		parts = append(parts, blockade.ContentPart{Kind: "text", Text: *text})
	}
	if *textFile != "" {
		data, err := readOperatorContentFile(*textFile)
		if err != nil {
			return err
		}
		parts = append(parts, blockade.ContentPart{Kind: "text", Text: string(data)})
	}
	if *imagePath != "" {
		data, err := readOperatorContentFile(*imagePath)
		if err != nil {
			return err
		}
		parts = append(parts, blockade.ContentPart{Kind: "image", MediaType: http.DetectContentType(data), Data: data})
	}
	if *audioPath != "" {
		if *audioType == "" {
			return errors.New("--audio-type is required with --audio")
		}
		data, err := readOperatorContentFile(*audioPath)
		if err != nil {
			return err
		}
		parts = append(parts, blockade.ContentPart{Kind: "audio", MediaType: *audioType, Data: data})
	}
	if *videoPath != "" {
		if *videoType == "" {
			return errors.New("--video-type is required with --video")
		}
		data, err := readOperatorContentFile(*videoPath)
		if err != nil {
			return err
		}
		parts = append(parts, blockade.ContentPart{Kind: "video", MediaType: *videoType, Data: data})
	}
	request := blockade.InterpretRequest{APIVersion: blockade.ContentAPIVersion, Parts: parts, Prompt: *prompt}
	if err := blockade.ValidateInterpretRequest(request); err != nil {
		return err
	}
	return sendOperatorRequest(*base, http.MethodPost, "/v1/interpretations", request, output, 3*time.Minute)
}

func readOperatorContentFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 10<<20 {
		return nil, errors.New("content file exceeds 10 MiB")
	}
	return data, nil
}
