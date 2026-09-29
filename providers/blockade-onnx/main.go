// blockade-provider-onnx is a standalone Blockade provider for YOLO ONNX
// detection models. Cymonkey launches it through the generic plugin transport.
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
	"slices"
	"strings"

	"blockade"
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
	engine   *onnxEngine
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
	case "blockade.model.inspect":
		var params struct {
			Settings map[string]string       `json:"settings,omitempty"`
			Model    blockade.InstalledModel `json:"model"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		err := inspectInstalledModel(params.Model, p.manifest.Name, params.Settings, true)
		result := map[string]any{
			"apiVersion": blockade.ProviderAdapterAPIVersion,
			"compatible": err == nil,
		}
		if err != nil {
			result["detail"] = err.Error()
		}
		return result, nil
	case "blockade.start":
		if p.engine != nil {
			return nil, errors.New("provider is already started")
		}
		var params struct {
			ID       string                   `json:"id"`
			Settings map[string]string        `json:"settings,omitempty"`
			Model    *blockade.InstalledModel `json:"model"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		if params.Model == nil {
			return nil, errors.New("ONNX provider requires an installed model")
		}
		config, err := engineConfigFrom(params.ID, *params.Model, p.manifest.Name, params.Settings)
		if err != nil {
			return nil, err
		}
		engine, err := startOnnxEngine(config)
		if err != nil {
			return nil, err
		}
		p.engine = engine
		return map[string]any{}, nil
	case "blockade.capabilities":
		return blockade.ProviderAdapterCapabilities{
			APIVersion:   blockade.ProviderAdapterAPIVersion,
			Capabilities: []string{blockade.CapabilityImageObserve, blockade.CapabilityObjectDetect},
		}, nil
	case "blockade.health":
		return blockade.ProviderAdapterHealth{
			APIVersion: blockade.ProviderAdapterAPIVersion,
			Ready:      p.engine != nil,
		}, nil
	case "blockade.observe":
		if p.engine == nil {
			return nil, errors.New("provider has not been started")
		}
		var params struct {
			Request blockade.ProviderAdapterObserveRequest `json:"request"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		response, err := p.engine.Observe(ctx, params.Request.Request)
		if err != nil {
			return nil, err
		}
		return blockade.ProviderAdapterObserveResponse{
			APIVersion: blockade.ProviderAdapterAPIVersion,
			Response:   response,
		}, nil
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
	if p.engine == nil {
		return nil
	}
	err := p.engine.Close()
	p.engine = nil
	return err
}

func inspectInstalledModel(model blockade.InstalledModel, providerName string, settings map[string]string, inspectRuntime bool) error {
	if err := model.Manifest.Validate(); err != nil {
		return err
	}
	if model.Manifest.Provider != providerName {
		return fmt.Errorf("model provider %q does not match %q", model.Manifest.Provider, providerName)
	}
	if !slices.Contains(model.Manifest.Capabilities, blockade.CapabilityObjectDetect) {
		return errors.New("ONNX model must declare object.detect")
	}
	path, err := modelArtifactPath(model, "yolo", ".onnx")
	if err != nil {
		return err
	}
	config, err := parseProviderSettings(settings)
	if err != nil {
		return err
	}
	if !inspectRuntime {
		return nil
	}
	if err := initializeOnnxRuntime(config.runtimeLibrary); err != nil {
		return fmt.Errorf("initialize ONNX Runtime: %w", err)
	}
	return inspectOnnxModel(path)
}

func modelArtifactPath(model blockade.InstalledModel, role, extension string) (string, error) {
	var matches []string
	for _, artifact := range model.Manifest.Artifacts {
		if artifact.Role == role {
			matches = append(matches, artifact.File)
		}
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("model needs exactly one %q artifact", role)
	}
	if !strings.EqualFold(filepath.Ext(matches[0]), extension) {
		return "", fmt.Errorf("model artifact %q must use %s", matches[0], extension)
	}
	path := filepath.Join(model.Directory, matches[0])
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("model artifact must be a regular file")
	}
	return path, nil
}
