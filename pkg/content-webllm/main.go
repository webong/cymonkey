// cymonkey-content-plugin-webllm exposes the browser-local WebLLM runtime through
// Cymonkey's public content plugin protocol.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"kit/contentkit"
	"os"
	"path/filepath"
	"strings"

	webllmruntime "cymonkey-content-plugin-webllm/runtime"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"github.com/webong/ext/pkg/plugin-go/contract/transport/jsonline"
)

const (
	contentContract        = "content.provider"
	contentContractVersion = "v1"
)

type provider struct {
	descriptor plugin.Descriptor
	adapter    contentkit.ProviderAdapter
}

func main() {
	descriptor, err := loadPluginDescriptor(filepath.Join(filepath.Dir(os.Args[0]), store.ManifestName))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := serveProvider(context.Background(), descriptor); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadPluginDescriptor(path string) (plugin.Descriptor, error) {
	descriptor, err := packagekit.LoadDescriptor(path)
	if err != nil {
		return plugin.Descriptor{}, err
	}
	manifest := packagekit.Manifest{Descriptor: descriptor}
	if !manifest.Supports(contentContract, contentContractVersion) {
		return plugin.Descriptor{}, errors.New("plugin manifest does not declare a supported content contract")
	}
	return descriptor, nil
}

func serveProvider(ctx context.Context, descriptor plugin.Descriptor) error {
	p := &provider{descriptor: descriptor}
	defer p.close()
	return jsonline.ServeFunc(ctx, descriptor, p.call, jsonline.FuncOptions{})
}

func (p *provider) call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "content.provider.start":
		if p.adapter != nil {
			return nil, errors.New("provider is already started")
		}
		var params struct {
			ID       string            `json:"id"`
			Settings map[string]string `json:"settings,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		adapter, err := webllmruntime.New(ctx, contentkit.ProviderAdapterRuntime{ID: params.ID, Settings: params.Settings})
		if err != nil {
			return nil, err
		}
		p.adapter = adapter
		return map[string]any{}, nil
	case "content.provider.capabilities":
		if p.adapter == nil {
			return contentkit.ProviderAdapterCapabilities{
				APIVersion:   contentkit.ProviderAdapterAPIVersion,
				Capabilities: []string{contentkit.CapabilityImageObserve, contentkit.CapabilityVisionLanguage},
			}, nil
		}
		return p.adapter.Capabilities(ctx)
	case "content.provider.health":
		if p.adapter == nil {
			return contentkit.ProviderAdapterHealth{APIVersion: contentkit.ProviderAdapterAPIVersion, Ready: false}, nil
		}
		return p.adapter.Health(ctx)
	case "content.provider.observe":
		if p.adapter == nil {
			return nil, errors.New("provider has not been started")
		}
		var params struct {
			Request contentkit.ProviderAdapterObserveRequest `json:"request"`
			// Blockade sends the adapter's resolved secrets with every call. An
			// image provider has none to use, but must still accept the field.
			Secrets map[string]string `json:"secrets,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return p.adapter.Observe(ctx, params.Request)
	case "content.provider.close":
		return map[string]any{}, p.close()
	default:
		return nil, fmt.Errorf("unsupported method %q", method)
	}
}

func decodeParams(raw json.RawMessage, destination any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return errors.New("method parameters are required")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func (p *provider) close() error {
	if p.adapter == nil {
		return nil
	}
	err := p.adapter.Close()
	p.adapter = nil
	return err
}
