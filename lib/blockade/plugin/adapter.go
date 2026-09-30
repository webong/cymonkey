// Package blockadeplugin adapts an installed provider package to Blockade's
// provider interface. Blockade still validates every normalized response.
package blockadeplugin

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync/atomic"
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
	launch, err := newLaunchPlan(installed)
	if err != nil {
		return err
	}
	factory := func(ctx context.Context, runtime blockade.ProviderAdapterRuntime) (blockade.ProviderAdapter, error) {
		pool := &processPool{}
		workers := runtime.Workers
		if workers == 0 {
			workers = 1
		}
		for range workers {
			proc, err := launch.open(ctx)
			if err != nil {
				_ = pool.Close()
				return nil, err
			}
			start := map[string]any{"id": runtime.ID, "settings": runtime.Settings}
			if runtime.Model != nil {
				start["model"] = runtime.Model
			}
			if err := proc.Call(ctx, "blockade.start", start, nil); err != nil {
				_ = proc.Close()
				_ = pool.Close()
				return nil, err
			}
			pool.processes = append(pool.processes, proc)
		}
		base := &adapter{pool: pool, runtime: runtime}
		reported, err := base.Capabilities(ctx)
		if err != nil {
			_ = pool.Close()
			return nil, err
		}
		image := slices.Contains(reported.Capabilities, blockade.CapabilityImageObserve) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityObjectDetect) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityObjectSegment) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityVisionLanguage)
		content := slices.Contains(reported.Capabilities, blockade.CapabilityTextInterpret) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityAudioInterpret) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityImageInterpret) ||
			slices.Contains(reported.Capabilities, blockade.CapabilityVideoInterpret) ||
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
		proc, err := launch.open(ctx)
		if err != nil {
			return err
		}
		defer proc.Close()
		return inspectModel(ctx, proc, runtime.Settings, model)
	}
	return registry.RegisterWithModelInspector(installed.Manifest.Name, factory, inspect)
}

type launchPlan struct {
	installed  providerplugin.Installed
	descriptor *blockade.ProviderPackage
}

func newLaunchPlan(installed providerplugin.Installed) (launchPlan, error) {
	plan := launchPlan{installed: installed}
	if installed.Manifest.Command != "" {
		return plan, nil
	}
	if err := installed.Verify(); err != nil {
		return launchPlan{}, err
	}
	descriptorPath, err := installed.Resource("blockade.json")
	if err != nil {
		return launchPlan{}, fmt.Errorf("Blockade source provider descriptor: %w", err)
	}
	descriptor, err := blockade.LoadProviderPackage(descriptorPath)
	if err != nil {
		return launchPlan{}, err
	}
	if _, err := installed.Resource(descriptor.Entrypoint); err != nil {
		return launchPlan{}, fmt.Errorf("Blockade source provider entrypoint: %w", err)
	}
	plan.descriptor = &descriptor
	return plan, nil
}

func (p launchPlan) open(ctx context.Context) (*providerplugin.Process, error) {
	if p.descriptor == nil {
		return providerplugin.Open(ctx, p.installed)
	}
	root, err := blockade.DefaultRuntimeRoot()
	if err != nil {
		return nil, err
	}
	runtime, err := blockade.ResolveInstalledRuntime(root, p.descriptor.Runtime.Name, p.descriptor.Runtime.Version)
	if err != nil {
		return nil, fmt.Errorf("resolve Blockade runtime %s@%s: %w", p.descriptor.Runtime.Name, p.descriptor.Runtime.Version, err)
	}
	entrypoint, err := p.installed.Resource(p.descriptor.Entrypoint)
	if err != nil {
		return nil, err
	}
	return providerplugin.OpenCommand(ctx, p.installed, providerplugin.Command{
		Executable: runtime.ExecutablePath(),
		Arguments:  []string{"-B", entrypoint},
		Environment: []string{
			"PYTHONNOUSERSITE=1",
			"PYTHONDONTWRITEBYTECODE=1",
			"BLOCKADE_PLUGIN_MANIFEST=" + filepath.Join(p.installed.Directory, "plugin.json"),
		},
	})
}

type processPool struct {
	processes []*providerplugin.Process
	next      atomic.Uint64
}

func (p *processPool) Call(ctx context.Context, method string, params, result any) error {
	if p == nil || len(p.processes) == 0 {
		return errors.New("Blockade provider process pool is empty")
	}
	index := int((p.next.Add(1) - 1) % uint64(len(p.processes)))
	return normalizeProcessError(p.processes[index].Call(ctx, method, params, result))
}

func normalizeProcessError(err error) error {
	if err == nil {
		return nil
	}
	var remote *providerplugin.RemoteError
	if !errors.As(err, &remote) {
		return err
	}
	kind := blockade.ProviderAdapterErrorKind(remote.Code)
	switch kind {
	case blockade.ProviderAdapterErrorAuthentication,
		blockade.ProviderAdapterErrorRateLimit,
		blockade.ProviderAdapterErrorTimeout,
		blockade.ProviderAdapterErrorCanceled,
		blockade.ProviderAdapterErrorUnavailable,
		blockade.ProviderAdapterErrorInvalidRequest,
		blockade.ProviderAdapterErrorInvalidResponse:
		mapped := blockade.NewProviderAdapterError(kind, errors.New(remote.Message))
		mapped.RetryAfter = time.Duration(remote.RetryAfterMilliseconds) * time.Millisecond
		return mapped
	default:
		return err
	}
}

func (p *processPool) Close() error {
	if p == nil {
		return nil
	}
	var result error
	for _, proc := range p.processes {
		result = errors.Join(result, proc.Close())
	}
	p.processes = nil
	return result
}

type adapter struct {
	pool    *processPool
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
	err := a.pool.Call(ctx, "blockade.observe", map[string]any{"request": request, "secrets": secrets}, &result)
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
	err := a.pool.Call(ctx, "blockade.interpret", map[string]any{"request": request, "secrets": secrets}, &result)
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
	err := a.pool.Call(ctx, "blockade.capabilities", nil, &value)
	return value, err
}
func (a *adapter) Health(ctx context.Context) (blockade.ProviderAdapterHealth, error) {
	var value blockade.ProviderAdapterHealth
	err := a.pool.Call(ctx, "blockade.health", nil, &value)
	return value, err
}
func (a *adapter) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, proc := range a.pool.processes {
		_ = proc.Call(ctx, "blockade.close", nil, nil)
	}
	return a.pool.Close()
}
