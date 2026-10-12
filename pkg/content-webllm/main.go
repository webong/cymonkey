// cymonkey-content-plugin-webllm exposes the browser-local WebLLM runtime through
// Cymonkey's public content plugin protocol.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blockade"
	webllmruntime "cymonkey-content-plugin-webllm/runtime"
	"github.com/webong/ext/pkg/plugin"
	"github.com/webong/ext/pkg/plugin/jsonline"
	"github.com/webong/ext/pkg/plugin/packagekit"
	"github.com/webong/ext/pkg/plugin/store"
	"wirebridge"
)

var contentWire = wirebridge.Mapping{
	PublicPrefix: "cymonkey.content.", InternalPrefix: "blockade.", NativePrefix: "blockade.provider.",
	ToInternal: map[string]string{
		"cymonkey.content-adapter/v1alpha1":    blockade.ProviderAdapterAPIVersion,
		"cymonkey.observation/v1alpha1":        blockade.APIVersion,
		"cymonkey.observation-result/v1alpha1": blockade.APIVersion,
		"cymonkey.content/v1alpha1":            blockade.ContentAPIVersion,
		"cymonkey.model/v1alpha1":              blockade.ModelManifestAPIVersion,
	},
	ToPublic: map[string]string{
		blockade.ProviderAdapterAPIVersion: "cymonkey.content-adapter/v1alpha1",
		blockade.APIVersion:                "cymonkey.observation-result/v1alpha1",
		blockade.ContentAPIVersion:         "cymonkey.content/v1alpha1",
		blockade.ModelManifestAPIVersion:   "cymonkey.model/v1alpha1",
	},
	KeysToInternal: map[string]string{"plugin": "provider"}, KeysToPublic: map[string]string{"provider": "plugin"},
}

const (
	blockadeContract        = "blockade.provider"
	cymonkeyContentContract = "cymonkey.content"
	blockadeContractVersion = "v1alpha1"
)

type provider struct {
	descriptor plugin.Descriptor
	adapter    blockade.ProviderAdapter
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
	if !manifest.Supports(blockadeContract, blockadeContractVersion) && !manifest.Supports(cymonkeyContentContract, blockadeContractVersion) {
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
	return contentWire.WrapCall(method, raw, func(method string, raw json.RawMessage) (any, error) {
		return p.callLibrary(ctx, method, raw)
	})
}

func (p *provider) callLibrary(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "blockade.start":
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
		adapter, err := webllmruntime.New(ctx, blockade.ProviderAdapterRuntime{ID: params.ID, Settings: params.Settings})
		if err != nil {
			return nil, err
		}
		p.adapter = adapter
		return map[string]any{}, nil
	case "blockade.capabilities":
		if p.adapter == nil {
			return blockade.ProviderAdapterCapabilities{
				APIVersion:   blockade.ProviderAdapterAPIVersion,
				Capabilities: []string{blockade.CapabilityImageObserve, blockade.CapabilityVisionLanguage},
			}, nil
		}
		return p.adapter.Capabilities(ctx)
	case "blockade.health":
		if p.adapter == nil {
			return blockade.ProviderAdapterHealth{APIVersion: blockade.ProviderAdapterAPIVersion, Ready: false}, nil
		}
		return p.adapter.Health(ctx)
	case "blockade.observe":
		if p.adapter == nil {
			return nil, errors.New("provider has not been started")
		}
		var params struct {
			Request blockade.ProviderAdapterObserveRequest `json:"request"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return p.adapter.Observe(ctx, params.Request)
	case "blockade.close":
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
