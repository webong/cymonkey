package consolecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	endpointmodels "cymonkey/src/console/generation"
	"cymonkey/src/console/modelguest"

	"github.com/webong/ext/pkg/ext-go/engine/host/store"
	"github.com/webong/ext/res/int/inference"
)

// modelBackend is what one configured model can do. The Console's own
// clients fill every field; an ext model adapter guest serves generation and
// embeddings only.
type modelBackend struct {
	generator   inference.Generator
	embedder    inference.Embedder
	transcriber inference.Transcriber
	synthesizer inference.Synthesizer
}

// missing names what this backend cannot serve for a tool kind, or "".
func (b *modelBackend) missing(kind string) string {
	switch {
	case kind == "embed" && b.embedder == nil:
		return "is served by a model adapter without embeddings"
	case kind == "transcribe" && b.transcriber == nil:
		return "is served by a model adapter, which does not transcribe yet"
	case kind == "synthesize" && b.synthesizer == nil:
		return "is served by a model adapter, which does not synthesize speech yet"
	}
	return ""
}

func (h *consoleHost) openBackend(ctx context.Context, model consoleModel) (*modelBackend, error) {
	if model.Adapter == "" {
		var key func(context.Context) (string, error)
		if model.APIKeyEnvironment != "" {
			key = endpointmodels.EnvironmentKey(model.APIKeyEnvironment)
		}
		client, err := endpointmodels.New(endpointmodels.Config{Protocol: model.Protocol, BaseURL: model.BaseURL, APIKey: key, EmbeddingInputType: model.EmbeddingInputType})
		if err != nil {
			return nil, err
		}
		return &modelBackend{generator: configuredGenerator{client: client, options: model.Options}, embedder: client, transcriber: client, synthesizer: client}, nil
	}
	if model.Protocol != "" || model.EmbeddingInputType != "" {
		return nil, fmt.Errorf("model %q uses adapter %q, so protocol and embeddingInputType must be empty", model.ID, model.Adapter)
	}
	if model.APIKeyEnvironment == "" {
		return nil, fmt.Errorf("model %q adapter needs apiKeyEnvironment", model.ID)
	}
	key := os.Getenv(model.APIKeyEnvironment)
	if key == "" {
		return nil, fmt.Errorf("API key environment variable %q is not set", model.APIKeyEnvironment)
	}
	root, err := consolePluginRoot()
	if err != nil {
		return nil, err
	}
	packages, err := store.New(root, store.Limits{})
	if err != nil {
		return nil, err
	}
	installed, err := packages.Load(model.Adapter)
	if err != nil {
		return nil, fmt.Errorf("model %q adapter %q: %w", model.ID, model.Adapter, err)
	}
	provider, err := modelguest.Open(ctx, installed, modelguest.Values{Endpoint: model.BaseURL, APIKey: key})
	if err != nil {
		return nil, fmt.Errorf("model %q adapter %q: %w", model.ID, model.Adapter, err)
	}
	h.adapters = append(h.adapters, provider)
	return &modelBackend{generator: configuredAdapter{provider: provider, options: model.Options}, embedder: provider}, nil
}

// configuredAdapter fixes the host's native options on every request. It
// does not implement inference.Streamer: ext's inference bridge does not yet
// return a streamed response, so streaming such a model is unsupported.
type configuredAdapter struct {
	provider *modelguest.Provider
	options  json.RawMessage
}

func (m configuredAdapter) Generate(ctx context.Context, r inference.GenerationRequest) (inference.GenerationResponse, error) {
	if len(r.Options) > 0 {
		return inference.GenerationResponse{}, errors.New("native options are fixed by the host binding")
	}
	r.Options = append(json.RawMessage(nil), m.options...)
	return m.provider.Generate(ctx, r)
}

// consolePluginRoot is the store that holds installed packages, the same one
// the host and the content service use.
func consolePluginRoot() (string, error) {
	if value := os.Getenv("CYMONKEY_PLUGIN_DIR"); value != "" {
		return filepath.Abs(value)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cymonkey", "plugins"), nil
}
