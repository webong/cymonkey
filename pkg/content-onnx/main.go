// cymonkey-content-plugin-onnx implements Cymonkey content inference for YOLO ONNX
// detection models. Cymonkey launches it through the generic plugin transport.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"blockade"
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
	engine     *onnxEngine
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
	case "content.provider.model.inspect":
		var params struct {
			Settings map[string]string       `json:"settings,omitempty"`
			Model    blockade.InstalledModel `json:"model"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		err := inspectInstalledModel(params.Model, p.descriptor.Identity.ID, params.Settings, true)
		result := map[string]any{
			"apiVersion": blockade.ProviderAdapterAPIVersion,
			"compatible": err == nil,
		}
		if err != nil {
			result["detail"] = err.Error()
		}
		return result, nil
	case "content.provider.start":
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
		config, err := engineConfigFrom(params.ID, *params.Model, p.descriptor.Identity.ID, params.Settings)
		if err != nil {
			return nil, err
		}
		engine, err := startOnnxEngine(config)
		if err != nil {
			return nil, err
		}
		p.engine = engine
		return map[string]any{}, nil
	case "content.provider.capabilities":
		return blockade.ProviderAdapterCapabilities{
			APIVersion:   blockade.ProviderAdapterAPIVersion,
			Capabilities: []string{blockade.CapabilityImageObserve, blockade.CapabilityObjectDetect},
		}, nil
	case "content.provider.health":
		return blockade.ProviderAdapterHealth{
			APIVersion: blockade.ProviderAdapterAPIVersion,
			Ready:      p.engine != nil,
		}, nil
	case "content.provider.observe":
		if p.engine == nil {
			return nil, errors.New("provider has not been started")
		}
		var params struct {
			Request blockade.ProviderAdapterObserveRequest `json:"request"`
			// Blockade sends the adapter's resolved secrets with every call. An
			// image provider has none to use, but must still accept the field.
			Secrets map[string]string `json:"secrets,omitempty"`
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
