// Package httpapi exposes caller-owned Console registries and model bindings.
// The host supplies authentication, origins, listeners and service lifetimes.
package httpapi

import (
	"contentkit"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"blockade"
	"console"
)

const APIVersion = "cymonkey.console/v1alpha1"

type Server struct {
	Tools  *console.Registry
	Models *blockade.ModelCatalog
}

func (s Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.Tools == nil || s.Models == nil {
		failure(w, 503, "unconfigured")
		return
	}
	switch r.URL.Path {
	case "/v1/console/tools":
		if r.Method != http.MethodGet {
			failure(w, 405, "method_not_allowed")
			return
		}
		reply(w, map[string]any{"apiVersion": APIVersion, "tools": s.Tools.Definitions()})
	case "/v1/console/models":
		if r.Method != http.MethodGet {
			failure(w, 405, "method_not_allowed")
			return
		}
		reply(w, map[string]any{"apiVersion": APIVersion, "models": s.Models.Models()})
	case "/v1/console/calls":
		if r.Method != http.MethodPost {
			failure(w, 405, "method_not_allowed")
			return
		}
		var call console.Call
		if decode(w, r, &call) != nil {
			failure(w, 400, "invalid_request")
			return
		}
		result, err := s.Tools.Invoke(r.Context(), call)
		if err != nil {
			failure(w, 422, code(err))
			return
		}
		reply(w, map[string]any{"apiVersion": APIVersion, "result": result})
	case "/v1/console/generations", "/v1/console/streams":
		if r.Method != http.MethodPost {
			failure(w, 405, "method_not_allowed")
			return
		}
		var request blockade.GenerationRequest
		if decode(w, r, &request) != nil || blockade.ValidateGenerationRequest(request) != nil || len(request.Tools) > 0 {
			failure(w, 400, "invalid_request")
			return
		}
		if _, ok := s.Models.Lookup(request.Model); !ok {
			failure(w, 422, "unknown_model")
			return
		}
		models := console.ModelTools{Model: s.Models, Tools: s.Tools}
		if r.URL.Path == "/v1/console/generations" {
			result, err := models.Generate(r.Context(), request)
			if err != nil {
				failure(w, 502, code(err))
				return
			}
			reply(w, map[string]any{"apiVersion": APIVersion, "response": result})
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, err := models.Stream(r.Context(), request, func(event blockade.GenerationEvent) error {
			if err := json.NewEncoder(w).Encode(event); err != nil {
				return err
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return r.Context().Err()
		})
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"kind": "error", "code": code(err)})
		}
	default:
		http.NotFound(w, r)
	}
}
func decode(w http.ResponseWriter, r *http.Request, out any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON object")
	}
	return nil
}
func code(err error) string {
	switch {
	case errors.Is(err, console.ErrInvalidCall):
		return "invalid_call"
	case errors.Is(err, console.ErrToolNotFound):
		return "unknown_tool"
	case errors.Is(err, console.ErrInvalidResult):
		return "invalid_result"
	}
	return string(contentkit.ClassifyModelError(err))
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"apiVersion": APIVersion, "code": code})
}
