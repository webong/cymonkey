// Package blockadeplugin adapts an installed executable to Blockade's
// provider interface. Blockade still validates every normalized response.
package blockadeplugin

import (
	"context"
	"fmt"
	"slices"
	"time"

	"blockade"
	"providerplugin"
)

const ContractName = "blockade.provider"
const ContractVersion = "v1alpha1"

// Kind is retained for source compatibility with the original manifest field.
// Deprecated: use ContractName.
const Kind = ContractName

// RegisterInstalled selects Blockade adapters from a shared installation inventory.
func RegisterInstalled(registry *blockade.ProviderAdapterRegistry, installed []providerplugin.Installed) error {
	for _, item := range installed {
		if item.Manifest.Supports(ContractName, ContractVersion) {
			if err := Register(registry, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func Register(registry *blockade.ProviderAdapterRegistry, installed providerplugin.Installed) error {
	factory := func(ctx context.Context, runtime blockade.ProviderAdapterRuntime) (blockade.ProviderAdapter, error) {
		proc, err := providerplugin.Open(ctx, installed)
		if err != nil {
			return nil, err
		}
		start := map[string]any{"id": runtime.ID, "settings": runtime.Settings}
		if runtime.Model != nil {
			start["model"] = runtime.Model
		}
		if err := proc.Call(ctx, "blockade.start", start, nil); err != nil {
			_ = proc.Close()
			return nil, err
		}
		base := &adapter{proc: proc, runtime: runtime}
		reported, err := base.Capabilities(ctx)
		if err != nil {
			_ = proc.Close()
			return nil, err
		}
		image := slices.Contains(reported.Capabilities, blockade.CapabilityImageObserve) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityObjectDetect) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityObjectSegment) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityVisionLanguage)
		content := slices.Contains(reported.Capabilities, blockade.CapabilityTextInterpret) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityAudioInterpret) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityImageInterpret) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityMultimodalInterpret)
		switch {
		case image && content:
			return &multiAdapter{base}, nil
		case image:
			return &imageAdapter{base}, nil
		case content:
			return &contentAdapter{base}, nil
		default:
			// Older image providers may advertise their own capability names.
			// They used blockade.observe before the content contract existed.
			return &imageAdapter{base}, nil
		}
	}
	inspect := func(ctx context.Context, runtime blockade.ProviderAdapterRuntime, model blockade.InstalledModel) error {
		proc, err := providerplugin.Open(ctx, installed)
		if err != nil {
			return err
		}
		defer proc.Close()
		return inspectModel(ctx, proc, runtime.Settings, model)
	}
	return registry.RegisterWithModelInspector(installed.Manifest.Name, factory, inspect)
}

type adapter struct {
	proc    *providerplugin.Process
	runtime blockade.ProviderAdapterRuntime
}

func (a *adapter) observe(ctx context.Context, request blockade.ProviderAdapterObserveRequest) (blockade.ProviderAdapterObserveResponse, error) {
	secrets := make(map[string]string, len(a.runtime.SecretNames))
	for _, name := range a.runtime.SecretNames {
		value, err := a.runtime.Secrets.Resolve(ctx, name)
		if err != nil {
			return blockade.ProviderAdapterObserveResponse{}, err
		}
		secrets[name] = value
	}
	var result blockade.ProviderAdapterObserveResponse
	err := a.proc.Call(ctx, "blockade.observe", map[string]any{"request": request, "secrets": secrets}, &result)
	return result, err
}
func (a *adapter) interpret(ctx context.Context, request blockade.ProviderAdapterInterpretRequest) (blockade.ProviderAdapterInterpretResponse, error) {
	secrets := make(map[string]string, len(a.runtime.SecretNames))
	for _, name := range a.runtime.SecretNames {
		value, err := a.runtime.Secrets.Resolve(ctx, name)
		if err != nil {
			return blockade.ProviderAdapterInterpretResponse{}, err
		}
		secrets[name] = value
	}
	var result blockade.ProviderAdapterInterpretResponse
	err := a.proc.Call(ctx, "blockade.interpret", map[string]any{"request": request, "secrets": secrets}, &result)
	return result, err
}

func inspectModel(ctx context.Context, proc *providerplugin.Process, settings map[string]string, model blockade.InstalledModel) error {
	var result struct {
		APIVersion string `json:"apiVersion"`
		Compatible bool   `json:"compatible"`
		Detail     string `json:"detail,omitempty"`
	}
	if err := proc.Call(ctx, "blockade.model.inspect", map[string]any{"settings": settings, "model": model}, &result); err != nil {
		return err
	}
	if result.APIVersion != blockade.ProviderAdapterAPIVersion {
		return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidResponse, fmt.Errorf("model inspection returned unsupported apiVersion %q", result.APIVersion))
	}
	if !result.Compatible {
		if result.Detail == "" {
			return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidRequest, fmt.Errorf("model inspection rejected by provider"))
		}
		return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidRequest, fmt.Errorf("model inspection rejected by provider: %s", result.Detail))
	}
	return nil
}

type imageAdapter struct{ *adapter }

func (a *imageAdapter) Observe(ctx context.Context, request blockade.ProviderAdapterObserveRequest) (blockade.ProviderAdapterObserveResponse, error) {
	return a.observe(ctx, request)
}

type contentAdapter struct{ *adapter }

func (a *contentAdapter) Observe(context.Context, blockade.ProviderAdapterObserveRequest) (blockade.ProviderAdapterObserveResponse, error) {
	return blockade.ProviderAdapterObserveResponse{}, blockade.ErrModalityUnsupported
}

func (a *contentAdapter) Interpret(ctx context.Context, request blockade.ProviderAdapterInterpretRequest) (blockade.ProviderAdapterInterpretResponse, error) {
	return a.interpret(ctx, request)
}

type multiAdapter struct{ *adapter }

func (a *multiAdapter) Observe(ctx context.Context, request blockade.ProviderAdapterObserveRequest) (blockade.ProviderAdapterObserveResponse, error) {
	return a.observe(ctx, request)
}
func (a *multiAdapter) Interpret(ctx context.Context, request blockade.ProviderAdapterInterpretRequest) (blockade.ProviderAdapterInterpretResponse, error) {
	return a.interpret(ctx, request)
}
func (a *adapter) Capabilities(ctx context.Context) (blockade.ProviderAdapterCapabilities, error) {
	var value blockade.ProviderAdapterCapabilities
	err := a.proc.Call(ctx, "blockade.capabilities", nil, &value)
	return value, err
}
func (a *adapter) Health(ctx context.Context) (blockade.ProviderAdapterHealth, error) {
	var value blockade.ProviderAdapterHealth
	err := a.proc.Call(ctx, "blockade.health", nil, &value)
	return value, err
}
func (a *adapter) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = a.proc.Call(ctx, "blockade.close", nil, nil)
	return a.proc.Close()
}
