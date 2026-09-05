package blockade

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeEngineError(w http.ResponseWriter, err error, fallbackStatus int) {
	kind, ok := ProviderAdapterErrorKindOf(err)
	if !ok {
		writeJSONError(w, fallbackStatus, err.Error())
		return
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
