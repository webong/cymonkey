// blockade-provider-webllm exposes the browser-local WebLLM runtime through
// Blockade's executable provider protocol.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"blockade"
	blockadewebllm "blockade-provider-webllm/runtime"
	"providerplugin"
)

const (
	blockadeContract        = "blockade.provider"
	blockadeContractVersion = "v1alpha1"
	maxFrame                = 24 << 20
)

type rpcRequest struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     uint64 `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type provider struct {
	manifest providerplugin.Manifest
	adapter  blockade.ProviderAdapter
}

func main() {
	manifest, err := loadPluginManifest(filepath.Join(filepath.Dir(os.Args[0]), "plugin.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := (&provider{manifest: manifest}).serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadPluginManifest(path string) (providerplugin.Manifest, error) {
	manifest, err := providerplugin.LoadManifest(path)
	if err != nil {
		return providerplugin.Manifest{}, err
	}
	if !manifest.Supports(blockadeContract, blockadeContractVersion) {
		return providerplugin.Manifest{}, errors.New("plugin manifest does not declare the Blockade provider contract")
	}
	return manifest, nil
}

func (p *provider) serve(ctx context.Context, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), maxFrame)
	writer := bufio.NewWriter(output)
	defer p.close()
	for scanner.Scan() {
		var request rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return fmt.Errorf("decode plugin request: %w", err)
		}
		result, err := p.call(ctx, request.Method, request.Params)
		response := rpcResponse{ID: request.ID, Result: result}
		if err != nil {
			response.Result = nil
			response.Error = err.Error()
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(encoded, '\n')); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (p *provider) call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "plugin.hello":
		return p.manifest, nil
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
		adapter, err := blockadewebllm.New(ctx, blockade.ProviderAdapterRuntime{ID: params.ID, Settings: params.Settings})
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
