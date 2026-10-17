package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"jangolova/contentkit/public"
	"net/http"
	"os"
	"strings"
	"time"

	cymonkeyhost "cymonkey/src/internal/host"
	"cymonkey/src/observation"
)

func (server *operatorServer) interpretationConnection() (string, string, error) {
	if server.interpretationURL != "" {
		return server.interpretationURL, server.interpretationToken, nil
	}
	if server.config == "" {
		return "", "", errors.New("start the operator with --content-url or --config")
	}
	config, err := cymonkeyhost.LoadConfig(server.config)
	if err != nil {
		return "", "", errors.New("interpretation service is unavailable in the operator configuration")
	}
	service, ok := config.Service(observation.InterpretationServiceContract)
	if !ok {
		return "", "", errors.New("interpretation service is unavailable in the operator configuration")
	}
	token := server.interpretationToken
	if token == "" {
		name := service.TokenEnvironment
		if name != "" {
			token = os.Getenv(name)
			if strings.TrimSpace(token) == "" {
				return "", "", fmt.Errorf("content token environment variable %q is not set", name)
			}
		}
	}
	return service.Endpoint, token, nil
}

func (server *operatorServer) handleInterpretation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		operatorError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	endpoint, token, err := server.interpretationConnection()
	if err != nil {
		operatorError(w, http.StatusServiceUnavailable, "interpretation_unconfigured", err.Error())
		return
	}
	var request public.ContentRequest
	if err := decodeOperatorJSONLimit(w, r, &request, 16<<20); err != nil {
		operatorError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if request.APIVersion == "" {
		request.APIVersion = public.ContentAPIVersion
	}
	if request.APIVersion != public.ContentAPIVersion {
		operatorError(w, http.StatusUnprocessableEntity, "invalid_content", "unsupported content apiVersion")
		return
	}
	if err := public.ValidateRequest(request); err != nil {
		operatorError(w, http.StatusUnprocessableEntity, "invalid_content", err.Error())
		return
	}
	result, err := (public.ServiceClient{BaseURL: endpoint, BearerToken: token}).Interpret(r.Context(), request)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, public.ErrModalityUnsupported) {
			status = http.StatusNotImplemented
			err = errors.New("requested content modality is unsupported")
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
	parts := make([]public.ContentPart, 0, 4)
	if *text != "" {
		parts = append(parts, public.ContentPart{Kind: "text", Text: *text})
	}
	if *textFile != "" {
		data, err := readOperatorContentFile(*textFile)
		if err != nil {
			return err
		}
		parts = append(parts, public.ContentPart{Kind: "text", Text: string(data)})
	}
	if *imagePath != "" {
		data, err := readOperatorContentFile(*imagePath)
		if err != nil {
			return err
		}
		parts = append(parts, public.ContentPart{Kind: "image", MediaType: http.DetectContentType(data), Data: data})
	}
	if *audioPath != "" {
		if *audioType == "" {
			return errors.New("--audio-type is required with --audio")
		}
		data, err := readOperatorContentFile(*audioPath)
		if err != nil {
			return err
		}
		parts = append(parts, public.ContentPart{Kind: "audio", MediaType: *audioType, Data: data})
	}
	if *videoPath != "" {
		if *videoType == "" {
			return errors.New("--video-type is required with --video")
		}
		data, err := readOperatorContentFile(*videoPath)
		if err != nil {
			return err
		}
		parts = append(parts, public.ContentPart{Kind: "video", MediaType: *videoType, Data: data})
	}
	request := public.ContentRequest{APIVersion: public.ContentAPIVersion, Parts: parts, Prompt: *prompt}
	if err := public.ValidateRequest(request); err != nil {
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
