package blockade

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RequireBearerToken protects a Blockade HTTP service with one caller-owned
// token. Deployment systems can rotate the token by restarting the service;
// the value is never included in responses or logs.
func RequireBearerToken(next http.Handler, token string) http.Handler {
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := []byte(r.Header.Get("Authorization"))
		if len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="content"`)
			writeJSONError(w, http.StatusUnauthorized, "authorization is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HTTPServer exposes a configured Blockade inference backend as a small
// standalone HTTP service. Provider-specific details stay behind Engine.
type HTTPServer struct {
	engine   Engine
	provider string
}

func NewHTTPServer(engine Engine, provider string) (*HTTPServer, error) {
	if engine == nil {
		return nil, errors.New("Blockade inference backend is required")
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "configured"
	}
	return &HTTPServer{engine: engine, provider: provider}, nil
}

func (s *HTTPServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/capabilities", s.capabilities)
	mux.HandleFunc("/v1/observe", s.observe)
	mux.HandleFunc("/v1/interpret", s.interpret)
	return mux
}

func (s *HTTPServer) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if reporter, ok := s.engine.(EngineHealthReporter); ok {
		health, err := reporter.Health(r.Context())
		if err != nil {
			writeEngineError(w, err, http.StatusServiceUnavailable)
			return
		}
		if !health.Ready {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "provider": s.provider})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "provider": s.provider})
}

func (s *HTTPServer) capabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	capabilities := []string{CapabilityImageObserve}
	if reporter, ok := s.engine.(EngineCapabilities); ok {
		var err error
		capabilities, err = reporter.Capabilities(r.Context())
		if err != nil {
			writeEngineError(w, err, http.StatusServiceUnavailable)
			return
		}
	}
	writeJSON(w, http.StatusOK, CapabilitiesResponse{
		APIVersion:   APIVersion,
		Provider:     s.provider,
		Capabilities: capabilities,
	})
}

func (s *HTTPServer) observe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request ObserveRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid observation request")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "observation request must contain one JSON value")
		return
	}
	if request.APIVersion != "" && request.APIVersion != APIVersion {
		writeJSONError(w, http.StatusBadRequest, "unsupported apiVersion")
		return
	}
	if len(request.Image) == 0 {
		writeJSONError(w, http.StatusBadRequest, "image is required")
		return
	}
	result, err := s.engine.Observe(r.Context(), request)
	if err != nil {
		writeEngineError(w, err, http.StatusBadGateway)
		return
	}
	if err := ValidateObserveResponse(result); err != nil {
		writeJSONError(w, http.StatusBadGateway, "engine returned invalid observation response")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *HTTPServer) interpret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request InterpretRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid interpretation request")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "interpretation request must contain one JSON value")
		return
	}
	if request.APIVersion == "" {
		request.APIVersion = ContentAPIVersion
	}
	if err := ValidateInterpretRequest(request); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	engine, ok := s.engine.(ContentEngine)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, ErrModalityUnsupported.Error())
		return
	}
	result, err := engine.Interpret(r.Context(), request)
	if err != nil {
		writeEngineError(w, err, http.StatusBadGateway)
		return
	}
	if err := ValidateInterpretResponse(result, request); err != nil {
		writeJSONError(w, http.StatusBadGateway, "engine returned invalid interpretation response")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeEngineError(w http.ResponseWriter, err error, fallbackStatus int) {
	if errors.Is(err, ErrModalityUnsupported) {
		writeJSONError(w, http.StatusNotImplemented, err.Error())
		return
	}
	kind, ok := ProviderAdapterErrorKindOf(err)
	if !ok {
		writeJSONError(w, fallbackStatus, err.Error())
		return
	}
	var adapterError *ProviderAdapterError
	if errors.As(err, &adapterError) && adapterError.RetryAfter > 0 {
		seconds := (adapterError.RetryAfter + time.Second - 1) / time.Second
		w.Header().Set("Retry-After", strconv.FormatInt(int64(seconds), 10))
	}
	status := fallbackStatus
	switch kind {
	case ProviderAdapterErrorRateLimit:
		status = http.StatusTooManyRequests
	case ProviderAdapterErrorTimeout:
		status = http.StatusGatewayTimeout
	case ProviderAdapterErrorCanceled:
		status = http.StatusRequestTimeout
	case ProviderAdapterErrorUnavailable:
		status = http.StatusServiceUnavailable
	case ProviderAdapterErrorInvalidRequest:
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error(), "kind": string(kind)})
}
