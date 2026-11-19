// Package modelguest runs an installed ext model adapter (an
// inference.provider/v1 package) as a guest of the Console and exposes it as
// ext inference interfaces.
//
// Only operations ext's adapters and bridge carry end to end are exposed:
// Generate and Embed. Streaming, speech and transcription stay with the
// Console's own clients until ext's inference bridge returns the final
// streamed response and the adapters implement those operations.
package modelguest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	plugin "github.com/webong/ext/pkg/ext-go/engine"
	"github.com/webong/ext/pkg/ext-go/engine/host"
	"github.com/webong/ext/pkg/ext-go/engine/host/process"
	"github.com/webong/ext/pkg/ext-go/engine/host/store"
	ginference "github.com/webong/ext/pkg/ext-go/inference"
	"github.com/webong/ext/res/int/inference"
)

// Values are the adapter's configuration, passed as the EXT_ADAPTER_VALUE_*
// environment ext's adapters read. Both are required by ext's adapters.
type Values struct {
	Endpoint string
	APIKey   string
}

// Provider is one running adapter guest.
type Provider struct {
	process *host.Process
	client  *ginference.Client
}

var (
	_ inference.Generator = (*Provider)(nil)
	_ inference.Embedder  = (*Provider)(nil)
)

// Open verifies and starts an installed inference.provider/v1 package. The
// package runs as `<executable> run` with only the given values added to its
// environment. Every call is limited to the operations the reviewed
// descriptor declares for the inference contract and ext.stream.
func Open(ctx context.Context, installed store.Installed, values Values) (*Provider, error) {
	if !declares(installed.Manifest.Descriptor, ginference.ContractName, ginference.ContractVersion) {
		return nil, fmt.Errorf("package %q does not declare %s/%s", installed.Manifest.Descriptor.Identity.ID, ginference.ContractName, ginference.ContractVersion)
	}
	if strings.TrimSpace(values.Endpoint) == "" || values.APIKey == "" {
		return nil, errors.New("model adapter needs an endpoint and an API key")
	}
	if strings.ContainsAny(values.Endpoint, "\x00\n") || strings.ContainsAny(values.APIKey, "\x00\n") {
		return nil, errors.New("model adapter values must be single-line")
	}
	executable, err := installed.Executable()
	if err != nil {
		return nil, err
	}
	proc, err := host.Open(ctx, installed, host.Options{
		Authorize: authorize,
		Launch: &process.Command{
			Path: executable,
			Args: []string{"run"},
			Env:  []string{"EXT_ADAPTER_VALUE_endpoint=" + values.Endpoint, "EXT_ADAPTER_VALUE_api_key=" + values.APIKey},
		},
	})
	if err != nil {
		return nil, err
	}
	return &Provider{process: proc, client: ginference.NewClient(caller{proc})}, nil
}

// Generate runs one non-streaming generation in the guest.
func (p *Provider) Generate(ctx context.Context, request inference.GenerationRequest) (inference.GenerationResponse, error) {
	return p.client.Generate(ctx, request)
}

// Embed runs one embedding request in the guest.
func (p *Provider) Embed(ctx context.Context, request inference.EmbeddingRequest) (inference.EmbeddingResponse, error) {
	return p.client.Embed(ctx, request)
}

// Models lists the models the adapter reports.
func (p *Provider) Models(ctx context.Context) ([]inference.ModelInfo, error) {
	return p.client.Models(ctx)
}

// Close ends the guest process.
func (p *Provider) Close() error {
	if p == nil || p.process == nil {
		return nil
	}
	return p.process.Close()
}

// caller adapts a host process to the contract-level caller ext's typed
// inference client uses.
type caller struct{ process *host.Process }

func (c caller) Call(ctx context.Context, contract plugin.ContractRef, operation string, payload json.RawMessage) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.process.Call(ctx, contract.Name+"."+operation, payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// authorize admits only the inference contract and ext.stream, which the
// inference contract's stream operations open.
func authorize(ctx context.Context, request plugin.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch request.Contract.Name {
	case ginference.ContractName, "ext.stream":
		return nil
	}
	return fmt.Errorf("%w: model adapter contract %q", plugin.ErrDenied, request.Contract.Name)
}

func declares(descriptor plugin.Descriptor, name, version string) bool {
	for _, contract := range descriptor.Contracts {
		if contract.Name == name && contract.Version == version {
			return true
		}
	}
	return false
}
