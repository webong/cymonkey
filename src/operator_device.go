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
	"cymonkey/src/contracts"
	"cymonkey/src/devicebridge"
)

const operatorDeviceStreamLimit = 16 << 20

func (server *operatorServer) deviceRegistry() (*board.Registry, error) {
	providers, err := server.deviceProviders()
	if err != nil {
		return nil, err
	}
	return board.NewRegistry(providers...)
}

func (server *operatorServer) handleDeviceInventory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		operatorError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	registry, err := server.deviceRegistry()
	if err != nil {
		operatorError(w, http.StatusServiceUnavailable, "device_unavailable", err.Error())
		return
	}
	devices, err := registry.List(r.Context())
	if err != nil {
		operatorError(w, http.StatusBadGateway, "device_list_failed", err.Error())
		return
	}
	operatorJSON(w, http.StatusOK, devicebridge.FromLibraryDevices(devices))
}

func (server *operatorServer) handleDeviceAction(w http.ResponseWriter, r *http.Request, stream bool) {
	if r.Method != http.MethodPost {
		operatorError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	var request contracts.DeviceActionRequest
	if err := decodeOperatorJSON(w, r, &request); err != nil {
		operatorError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(request.Open.AdapterID) == "" || strings.TrimSpace(request.Open.DeviceID) == "" || request.Action.Capability == "" {
		operatorError(w, http.StatusUnprocessableEntity, "invalid_request", "adapterId, deviceId, and action capability are required")
		return
	}
	if len(request.Open.Grant.Capabilities) == 0 || len(request.Open.Grant.Capabilities) != 1 || request.Open.Grant.Capabilities[0] != request.Action.Capability {
		operatorError(w, http.StatusUnprocessableEntity, "invalid_request", "grant must name only the requested capability")
		return
	}
	if len(request.Action.Input) == 0 {
		request.Action.Input = json.RawMessage(`{}`)
	}
	registry, err := server.deviceRegistry()
	if err != nil {
		operatorError(w, http.StatusServiceUnavailable, "device_unavailable", err.Error())
		return
	}
	connection, err := registry.Open(r.Context(), devicebridge.ToLibraryOpen(request.Open))
	if err != nil {
		operatorDeviceError(w, err)
		return
	}
	defer connection.Close(context.Background())
	if !stream {
		result, err := connection.Invoke(r.Context(), devicebridge.ToLibraryAction(request.Action))
		if err != nil {
			operatorDeviceError(w, err)
			return
		}
		operatorJSON(w, http.StatusOK, devicebridge.FromLibraryResult(result))
		return
	}
	content, err := connection.Stream(r.Context(), devicebridge.ToLibraryAction(request.Action))
	if err != nil {
		operatorDeviceError(w, err)
		return
	}
	defer content.Close()
	if content.Size() > operatorDeviceStreamLimit {
		operatorError(w, http.StatusRequestEntityTooLarge, "device_stream_too_large", "device stream exceeds the 16 MiB operator limit")
		return
	}
	data, err := board.ReadAll(content, operatorDeviceStreamLimit)
	if err != nil {
		operatorDeviceError(w, err)
		return
	}
	if content.Truncated() {
		w.Header().Set("X-Cymonkey-Truncated", "true")
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func operatorDeviceError(w http.ResponseWriter, err error) {
	publicErr := publicDeviceError(err)
	status := http.StatusBadGateway
	if errors.Is(err, board.ErrNotGranted) {
		status = http.StatusForbidden
	} else if errors.Is(err, board.ErrUnknownProvider) || errors.Is(err, board.ErrUnknownDevice) {
		status = http.StatusNotFound
	}
	operatorError(w, status, "device_action_failed", publicErr.Error())
}

func operatorDeviceClient(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("operator device requires devices, invoke, or stream")
	}
	command := args[0]
	if command != "devices" && command != "invoke" && command != "stream" {
		return errors.New("operator device requires devices, invoke, or stream")
	}
	flags := flag.NewFlagSet("cymy operator device "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	base := flags.String("url", defaultOperatorURL, "operator server URL")
	adapter := flags.String("adapter", "", "device adapter ID")
	device := flags.String("device", "", "device ID")
	capability := flags.String("capability", "", "device capability")
	resource := flags.String("resource", "", "approved resource ID")
	input := flags.String("input", "{}", "action input JSON")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("operator device accepts flags only")
	}
	if command == "devices" {
		return sendOperatorRequest(*base, http.MethodGet, "/v1/devices", nil, output, 30*time.Second)
	}
	if *adapter == "" || *device == "" || *capability == "" || !json.Valid([]byte(*input)) {
		return errors.New("operator device action requires --adapter, --device, --capability, and valid --input JSON")
	}
	grant := contracts.DeviceGrant{Capabilities: []string{*capability}}
	if *resource != "" {
		grant.ResourceIDs = []string{*resource}
	}
	path := "/v1/device-actions"
	if command == "stream" {
		path = "/v1/device-streams"
	}
	return sendOperatorRequest(*base, http.MethodPost, path, contracts.DeviceActionRequest{
		Open:   contracts.DeviceOpenRequest{AdapterID: *adapter, DeviceID: *device, Grant: grant},
		Action: contracts.DeviceAction{Capability: *capability, ResourceID: *resource, Input: json.RawMessage(*input)},
	}, output, 60*time.Second)
}
