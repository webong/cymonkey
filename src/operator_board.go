package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"strings"
	"time"

	"board"
)

const operatorBoardStreamLimit = 16 << 20

type boardOperatorRequest struct {
	Open   board.OpenRequest `json:"open"`
	Action board.Action      `json:"action"`
}

func (server *operatorServer) boardRegistry() (*board.Registry, error) {
	providers, err := server.boardProviders()
	if err != nil {
		return nil, err
	}
	return board.NewRegistry(providers...)
}

func (server *operatorServer) handleBoardDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		operatorError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	registry, err := server.boardRegistry()
	if err != nil {
		operatorError(w, http.StatusServiceUnavailable, "board_unavailable", err.Error())
		return
	}
	devices, err := registry.List(r.Context())
	if err != nil {
		operatorError(w, http.StatusBadGateway, "board_list_failed", err.Error())
		return
	}
	operatorJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (server *operatorServer) handleBoardAction(w http.ResponseWriter, r *http.Request, stream bool) {
	if r.Method != http.MethodPost {
		operatorError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	var request boardOperatorRequest
	if err := decodeOperatorJSON(w, r, &request); err != nil {
		operatorError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(request.Open.ProviderID) == "" || strings.TrimSpace(request.Open.DeviceID) == "" || request.Action.Capability == "" {
		operatorError(w, http.StatusUnprocessableEntity, "invalid_request", "providerId, deviceId, and action capability are required")
		return
	}
	if len(request.Open.Grant.Capabilities) == 0 || len(request.Open.Grant.Capabilities) != 1 || request.Open.Grant.Capabilities[0] != request.Action.Capability {
		operatorError(w, http.StatusUnprocessableEntity, "invalid_request", "grant must name only the requested capability")
		return
	}
	if len(request.Action.Input) == 0 {
		request.Action.Input = json.RawMessage(`{}`)
	}
	registry, err := server.boardRegistry()
	if err != nil {
		operatorError(w, http.StatusServiceUnavailable, "board_unavailable", err.Error())
		return
	}
	connection, err := registry.Open(r.Context(), request.Open)
	if err != nil {
		operatorBoardError(w, err)
		return
	}
	defer connection.Close(context.Background())
	if !stream {
		result, err := connection.Invoke(r.Context(), request.Action)
		if err != nil {
			operatorBoardError(w, err)
			return
		}
		operatorJSON(w, http.StatusOK, result)
		return
	}
	content, err := connection.Stream(r.Context(), request.Action)
	if err != nil {
		operatorBoardError(w, err)
		return
	}
	defer content.Close()
	if content.Size() > operatorBoardStreamLimit {
		operatorError(w, http.StatusRequestEntityTooLarge, "board_stream_too_large", "Board stream exceeds the 16 MiB operator limit")
		return
	}
	data, err := board.ReadAll(content, operatorBoardStreamLimit)
	if err != nil {
		operatorBoardError(w, err)
		return
	}
	if content.Truncated() {
		w.Header().Set("X-Board-Truncated", "true")
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func operatorBoardError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, board.ErrNotGranted) {
		status = http.StatusForbidden
	} else if errors.Is(err, board.ErrUnknownProvider) || errors.Is(err, board.ErrUnknownDevice) {
		status = http.StatusNotFound
	}
	operatorError(w, status, "board_action_failed", err.Error())
}

func operatorBoardClient(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("operator board requires devices, invoke, or stream")
	}
	command := args[0]
	if command != "devices" && command != "invoke" && command != "stream" {
		return errors.New("operator board requires devices, invoke, or stream")
	}
	flags := flag.NewFlagSet("cymonkey operator board "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	base := flags.String("url", defaultOperatorURL, "operator server URL")
	provider := flags.String("provider", "", "Board provider ID")
	device := flags.String("device", "", "Board device ID")
	capability := flags.String("capability", "", "Board capability")
	resource := flags.String("resource", "", "approved resource ID")
	input := flags.String("input", "{}", "action input JSON")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("operator board accepts flags only")
	}
	if command == "devices" {
		return sendOperatorRequest(*base, http.MethodGet, "/v1/board/devices", nil, output, 30*time.Second)
	}
	if *provider == "" || *device == "" || *capability == "" || !json.Valid([]byte(*input)) {
		return errors.New("operator board action requires --provider, --device, --capability, and valid --input JSON")
	}
	grant := board.Grant{Capabilities: []board.Capability{board.Capability(*capability)}}
	if *resource != "" {
		grant.ResourceIDs = []string{*resource}
	}
	path := "/v1/board/actions"
	if command == "stream" {
		path = "/v1/board/streams"
	}
	return sendOperatorRequest(*base, http.MethodPost, path, boardOperatorRequest{
		Open:   board.OpenRequest{ProviderID: *provider, DeviceID: *device, Grant: grant},
		Action: board.Action{Capability: board.Capability(*capability), ResourceID: *resource, Input: json.RawMessage(*input)},
	}, output, 60*time.Second)
}
