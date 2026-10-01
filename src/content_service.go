package main

import (
	"bytes"
	"cymonkey/src/contracts"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
)

// contentServiceContract converts Cymonkey's HTTP contract at the executable
// edge. The wrapped handler only receives the internal library contract.
func contentServiceContract(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && (r.URL.Path == "/v1/observe" || r.URL.Path == "/v1/interpret") {
			data, err := io.ReadAll(io.LimitReader(r.Body, (16<<20)+1))
			if err != nil || len(data) > 16<<20 {
				writeContentServiceError(w, http.StatusRequestEntityTooLarge, "content request exceeds 16 MiB")
				return
			}
			var request map[string]any
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			if err := decoder.Decode(&request); err != nil {
				writeContentServiceError(w, http.StatusBadRequest, "invalid content request")
				return
			}
			var trailing any
			if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
				writeContentServiceError(w, http.StatusBadRequest, "content request must contain one JSON object")
				return
			}
			expected := contracts.ContentAPIVersion
			if r.URL.Path == "/v1/observe" {
				expected = contracts.ObservationRequestVersion
			}
			if version, _ := request["apiVersion"].(string); version == "" {
				request["apiVersion"] = expected
			} else if version != expected {
				writeContentServiceError(w, http.StatusBadRequest, "unsupported content apiVersion")
				return
			}
			translated, err := json.Marshal(libraryContentJSON(request))
			if err != nil {
				writeContentServiceError(w, http.StatusBadRequest, "invalid content request")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(translated))
			r.ContentLength = int64(len(translated))
		}

		recorder := httptest.NewRecorder()
		next.ServeHTTP(recorder, r)
		for name, values := range recorder.Header() {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		body := recorder.Body.Bytes()
		if json.Valid(body) {
			var value any
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.UseNumber()
			if decoder.Decode(&value) == nil {
				value = sanitizeContentServiceJSON(publicContentJSON(value))
				if object, ok := value.(map[string]any); ok && r.URL.Path == "/capabilities" {
					object["apiVersion"] = contracts.ContentServiceVersion
				}
				body, _ = json.Marshal(value)
				body = append(body, '\n')
			}
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(body)
	})
}

func sanitizeContentServiceJSON(value any) any {
	switch value := value.(type) {
	case []any:
		for index := range value {
			value[index] = sanitizeContentServiceJSON(value[index])
		}
	case map[string]any:
		for key, item := range value {
			if text, ok := item.(string); ok && (key == "error" || key == "detail") {
				value[key] = publicContentText(text)
			} else {
				value[key] = sanitizeContentServiceJSON(item)
			}
		}
	}
	return value
}

func writeContentServiceError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
