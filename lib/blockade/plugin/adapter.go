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

// Protocol lets a host expose its own plugin wire contract while this package
// continues to implement Blockade's Go interfaces internally.
type Protocol struct {
	MethodPrefix          string
	AdapterAPIVersion     string
	ObservationRequestAPI string
	ObservationAPIVersion string
	ContentAPIVersion     string
	ModelAPIVersion       string
	ManifestEnvironment   string
	RuntimeRoot           string
	DescriptorFile        string
	DescriptorAPIVersion  string
}

func nativeProtocol() Protocol {
	return Protocol{
		MethodPrefix: "blockade", AdapterAPIVersion: blockade.ProviderAdapterAPIVersion,
		ObservationRequestAPI: blockade.APIVersion, ObservationAPIVersion: blockade.APIVersion,
		ContentAPIVersion: blockade.ContentAPIVersion,
		ModelAPIVersion:   blockade.ModelManifestAPIVersion, ManifestEnvironment: "BLOCKADE_PLUGIN_MANIFEST",
		DescriptorFile: "blockade.json", DescriptorAPIVersion: blockade.ProviderPackageAPIVersion,
	}
}

func (p Protocol) normalized() Protocol {
	native := nativeProtocol()
	if p.MethodPrefix == "" {
		p.MethodPrefix = native.MethodPrefix
	}
	if p.AdapterAPIVersion == "" {
		p.AdapterAPIVersion = native.AdapterAPIVersion
	}
	if p.ObservationAPIVersion == "" {
		p.ObservationAPIVersion = native.ObservationAPIVersion
	}
	if p.ObservationRequestAPI == "" {
		p.ObservationRequestAPI = native.ObservationRequestAPI
	}
	if p.ContentAPIVersion == "" {
		p.ContentAPIVersion = native.ContentAPIVersion
	}
	if p.ModelAPIVersion == "" {
		p.ModelAPIVersion = native.ModelAPIVersion
	}
	if p.ManifestEnvironment == "" {
		p.ManifestEnvironment = native.ManifestEnvironment
	}
	if p.DescriptorFile == "" {
		p.DescriptorFile = native.DescriptorFile
	}
	if p.DescriptorAPIVersion == "" {
		p.DescriptorAPIVersion = native.DescriptorAPIVersion
	}
	return p
}

func (p Protocol) method(name string) string { return p.MethodPrefix + "." + name }

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
	return RegisterWithProtocol(registry, installed, nativeProtocol())
}

func RegisterWithProtocol(registry *blockade.ProviderAdapterRegistry, installed providerplugin.Installed, protocol Protocol) error {
	protocol = protocol.normalized()
	launch, err := newLaunchPlan(installed, protocol)
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
			proc, err := launch.open(ctx, protocol)
			if err != nil {
				_ = pool.Close()
				return nil, err
			}
			start := map[string]any{"id": runtime.ID, "settings": runtime.Settings}
			if runtime.Model != nil {
				start["model"] = wireModel(*runtime.Model, protocol)
			}
			if err := proc.Call(ctx, protocol.method("start"), start, nil); err != nil {
				_ = proc.Close()
				_ = pool.Close()
				return nil, err
			}
			pool.processes = append(pool.processes, proc)
		}
		base := &adapter{pool: pool, runtime: runtime, protocol: protocol}
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
		proc, err := launch.open(ctx, protocol)
		if err != nil {
			return err
		}
		defer proc.Close()
		return inspectModel(ctx, proc, runtime.Settings, model, protocol)
	}
	return registry.RegisterWithModelInspector(installed.Manifest.Name, factory, inspect)
}

type launchPlan struct {
	installed  providerplugin.Installed
	descriptor *blockade.ProviderPackage
}

func newLaunchPlan(installed providerplugin.Installed, protocol Protocol) (launchPlan, error) {
	plan := launchPlan{installed: installed}
	if installed.Manifest.Command != "" {
		return plan, nil
	}
	if err := installed.Verify(); err != nil {
		return launchPlan{}, err
	}
	descriptorPath, err := installed.Resource(protocol.DescriptorFile)
	if err != nil {
		return launchPlan{}, fmt.Errorf("source plugin descriptor: %w", err)
	}
	descriptor, err := blockade.LoadProviderPackageWithAPIVersion(descriptorPath, protocol.DescriptorAPIVersion)
	if err != nil {
		return launchPlan{}, err
	}
	if _, err := installed.Resource(descriptor.Entrypoint); err != nil {
		return launchPlan{}, fmt.Errorf("source plugin entrypoint: %w", err)
	}
	plan.descriptor = &descriptor
	return plan, nil
}

func (p launchPlan) open(ctx context.Context, protocol Protocol) (*providerplugin.Process, error) {
	if p.descriptor == nil {
		return providerplugin.Open(ctx, p.installed)
	}
	root := protocol.RuntimeRoot
	if root == "" {
		var err error
		root, err = blockade.DefaultRuntimeRoot()
		if err != nil {
			return nil, err
		}
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
			protocol.ManifestEnvironment + "=" + filepath.Join(p.installed.Directory, "plugin.json"),
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
	pool     *processPool
	runtime  blockade.ProviderAdapterRuntime
	protocol Protocol
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
	request.APIVersion = a.protocol.AdapterAPIVersion
	request.Request.APIVersion = a.protocol.ObservationRequestAPI
	var result blockade.ProviderAdapterObserveResponse
	err := a.pool.Call(ctx, a.protocol.method("observe"), map[string]any{"request": request, "secrets": secrets}, &result)
	if err == nil {
		err = normalizeObserveResponse(&result, a.protocol)
	}
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
	request.APIVersion = a.protocol.AdapterAPIVersion
	request.Request.APIVersion = a.protocol.ContentAPIVersion
	var result blockade.ProviderAdapterInterpretResponse
	err := a.pool.Call(ctx, a.protocol.method("interpret"), map[string]any{"request": request, "secrets": secrets}, &result)
	if err == nil {
		err = normalizeInterpretResponse(&result, a.protocol)
	}
	return result, err
}

func inspectModel(ctx context.Context, proc *providerplugin.Process, settings map[string]string, model blockade.InstalledModel, protocol Protocol) error {
	var result struct {
		APIVersion string `json:"apiVersion"`
		Compatible bool   `json:"compatible"`
		Detail     string `json:"detail,omitempty"`
	}
	if err := proc.Call(ctx, protocol.method("model.inspect"), map[string]any{"settings": settings, "model": wireModel(model, protocol)}, &result); err != nil {
		return err
	}
	if result.APIVersion != protocol.AdapterAPIVersion {
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
	err := a.pool.Call(ctx, a.protocol.method("capabilities"), nil, &value)
	if err == nil {
		err = normalizeAdapterVersion(&value.APIVersion, a.protocol)
	}
	return value, err
}
func (a *adapter) Health(ctx context.Context) (blockade.ProviderAdapterHealth, error) {
	var value blockade.ProviderAdapterHealth
	err := a.pool.Call(ctx, a.protocol.method("health"), nil, &value)
	if err == nil {
		err = normalizeAdapterVersion(&value.APIVersion, a.protocol)
	}
	return value, err
}
func (a *adapter) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, proc := range a.pool.processes {
		_ = proc.Call(ctx, a.protocol.method("close"), nil, nil)
	}
	return a.pool.Close()
}

func normalizeAdapterVersion(value *string, protocol Protocol) error {
	if *value != protocol.AdapterAPIVersion {
		return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidResponse, fmt.Errorf("adapter returned unsupported apiVersion %q", *value))
	}
	*value = blockade.ProviderAdapterAPIVersion
	return nil
}

func normalizeObserveResponse(result *blockade.ProviderAdapterObserveResponse, protocol Protocol) error {
	if err := normalizeAdapterVersion(&result.APIVersion, protocol); err != nil {
		return err
	}
	if result.Response.APIVersion != protocol.ObservationAPIVersion {
		return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidResponse, fmt.Errorf("observation returned unsupported apiVersion %q", result.Response.APIVersion))
	}
	result.Response.APIVersion = blockade.APIVersion
	return nil
}

func normalizeInterpretResponse(result *blockade.ProviderAdapterInterpretResponse, protocol Protocol) error {
	if err := normalizeAdapterVersion(&result.APIVersion, protocol); err != nil {
		return err
	}
	if result.Response.APIVersion != protocol.ContentAPIVersion {
		return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidResponse, fmt.Errorf("content result returned unsupported apiVersion %q", result.Response.APIVersion))
	}
	result.Response.APIVersion = blockade.ContentAPIVersion
	return nil
}

func wireModel(model blockade.InstalledModel, protocol Protocol) map[string]any {
	return map[string]any{
		"manifest": map[string]any{
			"apiVersion": protocol.ModelAPIVersion,
			"name":       model.Manifest.Name, "version": model.Manifest.Version,
			"plugin": model.Manifest.Provider, "capabilities": model.Manifest.Capabilities,
			"artifacts": model.Manifest.Artifacts,
		},
		"directory": model.Directory,
	}
}
