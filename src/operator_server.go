package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

	"board"
	"cymonkey/src/internal/engineprovider"
	"cymonkey/src/internal/host"
	"cymonkey/src/internal/orchestrator"
	"cymonkey/src/internal/userscripts"
	"cymonkey/src/observation"
	"jangolova/browserextension"
)

type operatorServer struct {
	engine         *engineprovider.Service
	token          string
	store          string
	config         string
	blockadeURL    string
	mu             sync.Mutex
	active         map[string]*operatorBrowserSession
	boardProviders func() ([]board.Provider, error)
}

type operatorBrowserSession struct {
	target  string
	applied map[string]userscripts.Record
	mu      sync.Mutex
}

func newOperatorServer(registry *orchestrator.Registry, token, store, config string) (*operatorServer, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("CYMONKEY_OPERATOR_TOKEN is required")
	}
	server := &operatorServer{token: token, store: store, config: config, active: make(map[string]*operatorBrowserSession), boardProviders: boardPluginProviders}
	engine, err := engineprovider.NewService(registry, token, engineprovider.WithRecoveryHook(server.replayRecoveredUserscripts))
	if err != nil {
		return nil, err
	}
	server.engine = engine
	return server, nil
}

func (server *operatorServer) Close(ctx context.Context) error { return server.engine.Close(ctx) }

func (server *operatorServer) Routes() http.Handler {
	engineRoutes := server.engine.Routes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			engineRoutes.ServeHTTP(w, r)
			return
		}
		if !server.authorized(r) {
			operatorError(w, http.StatusUnauthorized, "unauthorized", "authorization is required")
			return
		}
		switch r.URL.Path {
		case "/v1/browser-targets":
			server.handleTargets(w, r)
		case "/v1/browser-sessions":
			server.handleBrowserSession(w, r)
		case "/v1/extensions/actions":
			server.handleExtension(w, r)
		case "/v1/observations":
			server.handleObservation(w, r)
		case "/v1/board/devices":
			server.handleBoardDevices(w, r)
		case "/v1/board/actions":
			server.handleBoardAction(w, r, false)
		case "/v1/board/streams":
			server.handleBoardAction(w, r, true)
		default:
			if strings.HasPrefix(r.URL.Path, "/v1/userscripts/") {
				server.handleUserscript(w, r)
				return
			}
			if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/instances/") {
				server.mu.Lock()
				delete(server.active, strings.TrimPrefix(r.URL.Path, "/v1/instances/"))
				server.mu.Unlock()
			}
			engineRoutes.ServeHTTP(w, r)
		}
	})
}

func (server *operatorServer) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	supplied := strings.TrimPrefix(header, prefix)
	return strings.HasPrefix(header, prefix) && len(supplied) == len(server.token) && subtle.ConstantTimeCompare([]byte(supplied), []byte(server.token)) == 1
}

func (server *operatorServer) handleTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		operatorError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET is required")
		return
	}
	targets, err := browserextension.DiscoverTargets()
	if err != nil {
		operatorError(w, http.StatusInternalServerError, "discovery_failed", err.Error())
		return
	}
	operatorJSON(w, http.StatusOK, map[string]any{"targets": targets})
}

func (server *operatorServer) handleBrowserSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		operatorError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	var request browserSessionRequest
	if err := decodeOperatorJSON(w, r, &request); err != nil {
		operatorError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	connection, userscriptsTarget, err := request.connectRequest()
	if err != nil {
		operatorError(w, http.StatusUnprocessableEntity, "browser_connection_unavailable", err.Error())
		return
	}
	result := server.engineRequest(r.Context(), http.MethodPost, "/v1/instances", connection)
	if result.Code != http.StatusCreated {
		copyRecordedResponse(w, result)
		return
	}
	if userscriptsTarget != "" {
		session := &operatorBrowserSession{target: userscriptsTarget}
		if err := server.syncUserscripts(r.Context(), connection.InstanceID, session); err != nil {
			server.engineRequest(r.Context(), http.MethodDelete, "/v1/instances/"+connection.InstanceID, nil)
			operatorError(w, http.StatusBadGateway, "userscript_replay_failed", err.Error())
			return
		}
		server.mu.Lock()
		server.active[connection.InstanceID] = session
		server.mu.Unlock()
	}
	copyRecordedResponse(w, result)
}

type extensionOperatorRequest struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func (server *operatorServer) handleExtension(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		operatorError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	var request extensionOperatorRequest
	if err := decodeOperatorJSON(w, r, &request); err != nil {
		operatorError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(request.Input) == 0 {
		request.Input = []byte("{}")
	}
	if request.Name == "extension.install" || request.Name == "extension.run" {
		input, err := parseExtensionActionInput(request.Input)
		if err != nil {
			operatorError(w, http.StatusUnprocessableEntity, "invalid_extension", err.Error())
			return
		}
		target, err := resolveActionTarget(input)
		if err != nil {
			operatorError(w, http.StatusUnprocessableEntity, "invalid_target", err.Error())
			return
		}
		if input.Source == "" || input.Revision == "" {
			operatorError(w, http.StatusUnprocessableEntity, "invalid_extension", "source and prepared revision are required")
			return
		}
		description, err := inspectExtensionSource(input.Source)
		if err != nil || description.Revision != input.Revision {
			operatorError(w, http.StatusUnprocessableEntity, "invalid_extension", "extension source does not match the prepared revision")
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		var runErr error
		if request.Name == "extension.run" {
			target.Headless = input.Headless == nil || *input.Headless
			runErr = browserextension.RunWithDevTools(r.Context(), target, input.Source, input.Destination, input.Revision, func(result browserextension.InstallResult) error {
				return json.NewEncoder(flushResponseWriter{w}).Encode(result)
			})
		} else {
			runErr = installNativeExtension(r.Context(), target, input.Source, input.Destination, input.Revision, flushResponseWriter{w})
		}
		if runErr != nil {
			_ = json.NewEncoder(w).Encode(engineprovider.ErrorResponse{Code: "extension_operation_failed", Message: runErr.Error()})
		}
		return
	}
	value, err := extensionAction(request.Name, request.Input)
	if err != nil {
		operatorError(w, http.StatusUnprocessableEntity, "extension_action_failed", err.Error())
		return
	}
	operatorJSON(w, http.StatusOK, value)
}

type observationOperatorRequest struct {
	InstanceID string `json:"instanceId"`
	Prompt     string `json:"prompt,omitempty"`
	FullPage   bool   `json:"fullPage,omitempty"`
	ApprovalID string `json:"approvalId,omitempty"`
}

func (server *operatorServer) handleObservation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		operatorError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	if server.config == "" && server.blockadeURL == "" {
		operatorError(w, http.StatusServiceUnavailable, "observation_unconfigured", "start the operator with --blockade-url or --config to enable observations")
		return
	}
	var request observationOperatorRequest
	if err := decodeOperatorJSON(w, r, &request); err != nil {
		operatorError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if request.InstanceID == "" {
		operatorError(w, http.StatusUnprocessableEntity, "invalid_request", "instanceId is required")
		return
	}
	blockadeURL := server.blockadeURL
	if blockadeURL == "" {
		config, err := cymonkeyhost.LoadConfig(server.config)
		if err != nil || config.Observation == nil {
			operatorError(w, http.StatusServiceUnavailable, "observation_unconfigured", "observation configuration is unavailable")
			return
		}
		blockadeURL = config.Observation.Blockade.Endpoint
	}
	coordinator, err := observation.NewObservationCoordinatorWithCapture(cymonkeyhost.ObservationConfig{Blockade: cymonkeyhost.ObservationBlockadeConfig{Endpoint: blockadeURL}}, server.captureLocal)
	if err != nil {
		operatorError(w, http.StatusServiceUnavailable, "observation_unconfigured", err.Error())
		return
	}
	value, err := coordinator.Observe(r.Context(), observation.ObservationRequest{InstanceID: request.InstanceID, Prompt: request.Prompt, FullPage: request.FullPage, ApprovalID: request.ApprovalID})
	if err != nil {
		operatorError(w, http.StatusBadGateway, "observation_failed", err.Error())
		return
	}
	operatorJSON(w, http.StatusOK, value)
}

func (server *operatorServer) captureLocal(ctx context.Context, request observation.ObservationRequest) ([]byte, error) {
	result := server.engineRequest(ctx, http.MethodPost, "/v1/instances/"+url.PathEscape(request.InstanceID)+"/call", map[string]any{
		"method":     "act",
		"params":     map[string]any{"name": "window.screenshot", "input": map[string]any{"fullPage": request.FullPage}},
		"approvalId": request.ApprovalID,
	})
	if result.Code != http.StatusOK {
		return nil, fmt.Errorf("capture returned HTTP %d", result.Code)
	}
	var payload struct {
		Result struct {
			PNGBase64 string `json:"pngBase64"`
		} `json:"result"`
	}
	if result.Body.Len() > 17<<20 || json.Unmarshal(result.Body.Bytes(), &payload) != nil {
		return nil, errors.New("capture returned invalid JSON")
	}
	image, err := base64.StdEncoding.DecodeString(payload.Result.PNGBase64)
	if err != nil || len(image) == 0 || len(image) > 16<<20 {
		return nil, errors.New("capture returned an invalid PNG")
	}
	return image, nil
}

func (server *operatorServer) engineRequest(ctx context.Context, method, path string, value any) *httptest.ResponseRecorder {
	var body io.Reader
	if value != nil {
		data, _ := json.Marshal(value)
		body = bytes.NewReader(data)
	}
	request := httptest.NewRequestWithContext(ctx, method, path, body)
	request.Header.Set("Authorization", "Bearer "+server.token)
	request.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	server.engine.Routes().ServeHTTP(result, request)
	return result
}

func decodeOperatorJSON(w http.ResponseWriter, r *http.Request, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func operatorJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func operatorError(w http.ResponseWriter, status int, code, message string) {
	operatorJSON(w, status, engineprovider.ErrorResponse{Code: code, Message: message})
}

func copyRecordedResponse(w http.ResponseWriter, result *httptest.ResponseRecorder) {
	for name, values := range result.Header() {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(result.Code)
	_, _ = io.Copy(w, result.Body)
}

type flushResponseWriter struct{ http.ResponseWriter }

func (writer flushResponseWriter) Write(data []byte) (int, error) {
	n, err := writer.ResponseWriter.Write(data)
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}
