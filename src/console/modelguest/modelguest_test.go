package modelguest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/webong/ext/pkg/ext-go/engine/host/store"
	ginference "github.com/webong/ext/pkg/ext-go/inference"
	"github.com/webong/ext/res/int/inference"
)

// installOpenAIAdapter builds ext's OpenAI adapter at the version pinned as a
// tool in the root go.mod, packages it the way ext does, and installs it.
func installOpenAIAdapter(t *testing.T) store.Installed {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test location")
	}
	work := t.TempDir()
	binary := filepath.Join(work, "openai")
	build := exec.Command("go", "build", "-o", binary, "github.com/webong/ext/adapters/openai")
	build.Dir = filepath.Join(filepath.Dir(file), "..", "..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build adapter: %v\n%s", err, output)
	}
	packageDir := filepath.Join(work, "package")
	if _, err := ginference.PackageExecutable(ginference.ExecutablePackage{ID: "openai", Version: "1.0.0", Binary: binary, OutputDir: packageDir}); err != nil {
		t.Fatalf("package adapter: %v", err)
	}
	packages, err := store.New(filepath.Join(work, "store"), store.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := packages.Install(packageDir)
	if err != nil {
		t.Fatalf("install adapter: %v", err)
	}
	return installed
}

type fakeOpenAI struct {
	mu            sync.Mutex
	authorization []string
	chat          map[string]any
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.authorization = append(f.authorization, r.Header.Get("Authorization"))
	f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	switch r.URL.Path {
	case "/v1/chat/completions":
		var request map[string]any
		_ = json.Unmarshal(body, &request)
		f.mu.Lock()
		f.chat = request
		f.mu.Unlock()
		if _, tools := request["tools"]; tools {
			io.WriteString(w, `{"id":"c2","model":"m1","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"key\":\"a\"}"}}]}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
			return
		}
		io.WriteString(w, `{"id":"c1","model":"m1","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"note a"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	case "/v1/embeddings":
		io.WriteString(w, `{"model":"e1","data":[{"index":0,"embedding":[0.1,0.2]},{"index":1,"embedding":[0.3,0.4]}],"usage":{"prompt_tokens":4,"total_tokens":4}}`)
	default:
		http.NotFound(w, r)
	}
}

func TestOpenAIAdapterGuestGeneratesAndEmbeds(t *testing.T) {
	installed := installOpenAIAdapter(t)
	fake := &fakeOpenAI{}
	server := httptest.NewServer(fake)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider, err := Open(ctx, installed, Values{Endpoint: server.URL + "/v1", APIKey: "guest-key"})
	if err != nil {
		t.Fatalf("open adapter guest: %v", err)
	}
	defer provider.Close()

	messages := []inference.Message{{Role: inference.RoleUser, Parts: []inference.Part{{Kind: inference.PartKindText, Text: "find a"}}}}
	response, err := provider.Generate(ctx, inference.GenerationRequest{Model: "m1", Messages: messages})
	if err != nil {
		t.Fatalf("generate through guest: %v", err)
	}
	if response.Validate() != nil || response.Text() != "note a" || response.StopReason != inference.FinishReasonStop || response.Usage.Total != 5 {
		t.Fatalf("response = %+v", response)
	}

	embeddings, err := provider.Embed(ctx, inference.EmbeddingRequest{Model: "e1", Inputs: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("embed through guest: %v", err)
	}
	if embeddings.Validate() != nil || len(embeddings.Embeddings) != 2 || embeddings.Embeddings[1].Vector[0] != 0.3 {
		t.Fatalf("embeddings = %+v", embeddings)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, header := range fake.authorization {
		if header != "Bearer guest-key" {
			t.Fatalf("adapter sent Authorization %q", header)
		}
	}
	if fake.chat["model"] != "m1" || len(fake.authorization) != 2 {
		t.Fatalf("chat request %v, requests %d", fake.chat, len(fake.authorization))
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("close guest: %v", err)
	}
}

func TestOpenRefusesPackagesAndValuesOutsideTheContract(t *testing.T) {
	installed := installOpenAIAdapter(t)
	ctx := context.Background()
	if _, err := Open(ctx, installed, Values{Endpoint: "http://127.0.0.1:1/v1"}); err == nil {
		t.Fatal("a missing API key must be refused")
	}
	if _, err := Open(ctx, installed, Values{Endpoint: "http://127.0.0.1:1/v1\nX: y", APIKey: "k"}); err == nil {
		t.Fatal("a multi-line value must be refused")
	}
	other := installed
	other.Manifest.Descriptor.Contracts = nil
	if _, err := Open(ctx, other, Values{Endpoint: "http://127.0.0.1:1/v1", APIKey: "k"}); err == nil {
		t.Fatal("a package without inference.provider/v1 must be refused")
	}
}

// ext's OpenAI adapter v0.1.0 reads tool-call arguments from function.parameters
// instead of function.arguments, so its tool calls arrive without arguments.
// The response fails ext's own validation, and the Console must refuse it
// rather than propose a call with lost arguments.
func TestAdapterToolCallsWithoutArgumentsAreRefused(t *testing.T) {
	installed := installOpenAIAdapter(t)
	server := httptest.NewServer(&fakeOpenAI{})
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider, err := Open(ctx, installed, Values{Endpoint: server.URL + "/v1", APIKey: "guest-key"})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	tools := []inference.ToolDefinition{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	messages := []inference.Message{{Role: inference.RoleUser, Parts: []inference.Part{{Kind: inference.PartKindText, Text: "find a"}}}}
	response, err := provider.Generate(ctx, inference.GenerationRequest{Model: "m1", Messages: messages, Tools: tools})
	if err != nil {
		return // a later adapter may refuse it itself
	}
	if response.Validate() == nil && string(response.Message.ToolCalls[0].Arguments) == `{"key":"a"}` {
		t.Skip("ext's adapter now returns tool-call arguments; drop this guard")
	}
	if response.Validate() == nil {
		t.Fatalf("a tool call without its arguments passed validation: %+v", response)
	}
}
