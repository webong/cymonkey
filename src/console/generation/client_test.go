package generation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/webong/ext/res/int/inference"
)

// openAIServer answers the OpenAI-compatible endpoints the client uses and
// records each request body by path.
func openAIServer(t *testing.T) (*Client, map[string]map[string]any) {
	t.Helper()
	seen := map[string]map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode %s: %v", r.URL.Path, err)
			}
			seen[r.URL.Path] = body
		} else if err := r.ParseMultipartForm(1 << 20); err == nil {
			seen[r.URL.Path] = map[string]any{"response_format": r.FormValue("response_format")}
		}
		switch r.URL.Path {
		case "/v1/chat/completions":
			if stream, _ := seen[r.URL.Path]["stream"].(bool); stream {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, `data: {"id":"s1","model":"m1","choices":[{"index":0,"delta":{"role":"assistant","content":"hel"}}]}`+"\n\n")
				io.WriteString(w, `data: {"id":"s1","model":"m1","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":"stop"}]}`+"\n\n")
				io.WriteString(w, "data: [DONE]\n\n")
				return
			}
			io.WriteString(w, `{"id":"c1","model":"m1","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"key\":\"a\"}"}}]}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
		case "/v1/embeddings":
			io.WriteString(w, `{"model":"e1","data":[{"index":1,"embedding":[0.3,0.4]},{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":4,"total_tokens":4}}`)
		case "/v1/audio/speech":
			io.WriteString(w, "ID3-audio")
		case "/v1/audio/transcriptions":
			io.WriteString(w, `{"text":"hello","language":"en","duration":1.5}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client, err := New(Config{Protocol: OpenAI, BaseURL: server.URL + "/v1", APIKey: func(context.Context) (string, error) { return "test", nil }})
	if err != nil {
		t.Fatal(err)
	}
	return client, seen
}

func userText(text string) []inference.Message {
	return []inference.Message{{Role: inference.RoleUser, Parts: []inference.Part{{Kind: inference.PartKindText, Text: text}}}}
}

func TestGenerateMapsToolCallsOntoExtFinishReason(t *testing.T) {
	client, _ := openAIServer(t)
	tools := []inference.ToolDefinition{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	response, err := client.Generate(context.Background(), inference.GenerationRequest{Model: "m1", Messages: userText("find a"), Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	if response.StopReason != inference.FinishReasonToolCalls || !response.HasToolCalls() || response.Message.ToolCalls[0].Name != "lookup" || response.Usage.Total != 5 {
		t.Fatalf("response = %+v", response)
	}
}

func TestStreamDeliversDeltasThroughAnEventSink(t *testing.T) {
	client, _ := openAIServer(t)
	var kinds []string
	var text strings.Builder
	response, err := client.Stream(context.Background(), inference.GenerationRequest{Model: "m1", Messages: userText("hi")}, inference.SinkFunc(func(_ context.Context, event inference.GenerationEvent) error {
		kinds = append(kinds, event.Kind)
		if event.Kind == "text.delta" {
			text.WriteString(event.Text)
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if text.String() != "hello" || response.Text() != "hello" || response.StopReason != inference.FinishReasonStop || kinds[0] != "started" {
		t.Fatalf("text %q, response %+v, kinds %v", text.String(), response, kinds)
	}
}

func TestEmbedReadsDimensionsFromOptionsAndRestoresOrder(t *testing.T) {
	client, seen := openAIServer(t)
	response, err := client.Embed(context.Background(), inference.EmbeddingRequest{Model: "e1", Inputs: []string{"a", "b"}, Options: json.RawMessage(`{"dimensions":2}`)})
	if err != nil {
		t.Fatal(err)
	}
	if seen["/v1/embeddings"]["dimensions"] != float64(2) || len(response.Embeddings) != 2 || response.Embeddings[0].Vector[0] != 0.1 {
		t.Fatalf("request %v, response %+v", seen["/v1/embeddings"], response)
	}
	if _, err := client.Embed(context.Background(), inference.EmbeddingRequest{Model: "e1", Inputs: []string{"a"}, Options: json.RawMessage(`{"tokens":[[1]]}`)}); !errors.Is(err, inference.ErrInvalidRequest) {
		t.Fatalf("unknown embedding option: %v", err)
	}
}

func TestSynthesizeSendsInstructionsAndClosesOnce(t *testing.T) {
	client, seen := openAIServer(t)
	stream, err := client.Synthesize(context.Background(), inference.SpeechRequest{Model: "s1", Input: "hi", Voice: "v", Format: "mp3", Options: json.RawMessage(`{"instructions":"calm"}`)})
	if err != nil {
		t.Fatal(err)
	}
	audio, err := io.ReadAll(stream)
	if err != nil || string(audio) != "ID3-audio" {
		t.Fatalf("audio %q, %v", audio, err)
	}
	if err := errors.Join(stream.Close(), stream.Close()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if body := seen["/v1/audio/speech"]; body["input"] != "hi" || body["instructions"] != "calm" {
		t.Fatalf("speech request %v", body)
	}
}

func TestTranscribeReadsMetadataFromOptions(t *testing.T) {
	client, seen := openAIServer(t)
	response, err := client.Transcribe(context.Background(), inference.TranscriptionRequest{Model: "t1", Audio: []byte("RIFF"), MediaType: "audio/wav", Options: json.RawMessage(`{"metadata":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "hello" || seen["/v1/audio/transcriptions"]["response_format"] != "verbose_json" {
		t.Fatalf("response %+v, request %v", response, seen["/v1/audio/transcriptions"])
	}
}

func TestFinishReasonRefusesWhatExtCannotRepresent(t *testing.T) {
	for native, want := range map[string]inference.FinishReason{"end_turn": inference.FinishReasonStop, "max_tokens": inference.FinishReasonLength, "tool_use": inference.FinishReasonToolCalls, "SAFETY": inference.FinishReasonContentFilter, "": ""} {
		if got, err := finishReason(native); err != nil || got != want {
			t.Errorf("finishReason(%q) = %q, %v", native, got, err)
		}
	}
	if _, err := finishReason("pause_turn"); err == nil {
		t.Error("an unknown finish reason must be refused")
	}
}
