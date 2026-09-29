package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"blockade"
	"cymonkey/src/internal/orchestrator"
)

func TestOperatorInterpretsTextAndAudioThroughBlockade(t *testing.T) {
	requests := make(chan blockade.InterpretRequest, 2)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/interpret" {
			t.Errorf("Blockade path = %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var request blockade.InterpretRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode Blockade request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		requests <- request
		_ = json.NewEncoder(w).Encode(blockade.InterpretResponse{
			APIVersion: blockade.ContentAPIVersion, RequestID: request.RequestID,
			Findings: []blockade.ContentFinding{{Kind: "summary", Text: "fixture result", SourceParts: []int{0}}},
		})
	}))
	defer backend.Close()
	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	server.blockadeURL = backend.URL
	unauthorized := operatorTestRequest(server, "", http.MethodPost, "/v1/interpretations", `{"parts":[{"kind":"text","text":"hello"}]}`)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized interpretation = %d", unauthorized.Code)
	}
	interpreted := operatorTestRequest(server, "secret", http.MethodPost, "/v1/interpretations", `{"parts":[{"kind":"text","text":"meeting notes"}]}`)
	if interpreted.Code != http.StatusOK || !bytes.Contains(interpreted.Body.Bytes(), []byte("fixture result")) {
		t.Fatalf("text interpretation = %d %s", interpreted.Code, interpreted.Body.String())
	}
	if request := <-requests; request.APIVersion != blockade.ContentAPIVersion || request.Parts[0].Text != "meeting notes" {
		t.Fatalf("forwarded text = %#v", request)
	}

	operatorHTTP := httptest.NewServer(server.Routes())
	defer operatorHTTP.Close()
	t.Setenv("CYMONKEY_OPERATOR_TOKEN", "secret")
	audioPath := filepath.Join(t.TempDir(), "speech.wav")
	if err := os.WriteFile(audioPath, []byte("recorded audio"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := operatorInterpretClient([]string{"--url", operatorHTTP.URL, "--audio", audioPath, "--audio-type", "audio/wav", "--prompt", "transcribe"}, &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("fixture result")) {
		t.Fatalf("audio interpretation output = %s", output.String())
	}
	if request := <-requests; len(request.Parts) != 1 || request.Parts[0].Kind != "audio" || string(request.Parts[0].Data) != "recorded audio" {
		t.Fatalf("forwarded audio = %#v", request)
	}
}
