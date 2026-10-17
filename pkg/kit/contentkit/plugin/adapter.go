// Package contentkitplugin adapts an installed content.provider package to the
// kit's provider interface. The kit still validates every normalized response.
package contentkitplugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host"
	"github.com/webong/ext/pkg/plugin-go/contract/host/process"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"kit/contentkit"
)

const ContractName = "content.provider"
const ContractVersion = "v1"

// Operations lists every method a Blockade provider package serves.
var Operations = []string{"start", "observe", "interpret", "capabilities", "health", "close", "model.inspect"}

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
		MethodPrefix: ContractName, AdapterAPIVersion: contentkit.ProviderAdapterAPIVersion,
		ObservationRequestAPI: contentkit.APIVersion, ObservationAPIVersion: contentkit.APIVersion,
		ContentAPIVersion: contentkit.ContentAPIVersion,
		ModelAPIVersion:   contentkit.ModelManifestAPIVersion, ManifestEnvironment: "PLUGIN_MANIFEST",
		DescriptorFile: "content.json", DescriptorAPIVersion: contentkit.ProviderPackageAPIVersion,
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
func RegisterInstalled(registry *contentkit.ProviderAdapterRegistry, installed []store.Installed) error {
	for _, item := range installed {
		if item.Supports(ContractName, ContractVersion) {
			if err := Register(registry, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func Register(registry *contentkit.ProviderAdapterRegistry, installed store.Installed) error {
	return RegisterWithProtocol(registry, installed, nativeProtocol())
}

// RegisterWithRuntimeRoot registers a package whose managed runtimes live under
// root instead of Blockade's default runtime directory.
func RegisterWithRuntimeRoot(registry *contentkit.ProviderAdapterRegistry, installed store.Installed, root string) error {
	protocol := nativeProtocol()
	protocol.RuntimeRoot = root
	return RegisterWithProtocol(registry, installed, protocol)
}

func RegisterWithProtocol(registry *contentkit.ProviderAdapterRegistry, installed store.Installed, protocol Protocol) error {
	protocol = protocol.normalized()
	launch, err := newLaunchPlan(installed, protocol)
	if err != nil {
		return err
	}
	factory := func(ctx context.Context, runtime contentkit.ProviderAdapterRuntime) (contentkit.ProviderAdapter, error) {
		pool := &processPool{}
		workers := runtime.Workers
		if workers == 0 {
			workers = 1
		}
		for range workers {
			proc, err := launch.open(ctx, protocol, workerPolicy())
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
		image := slices.Contains(reported.Capabilities, contentkit.CapabilityImageObserve) ||
			slices.Contains(reported.Capabilities, contentkit.CapabilityObjectDetect) ||
			slices.Contains(reported.Capabilities, contentkit.CapabilityObjectSegment) ||
			slices.Contains(reported.Capabilities, contentkit.CapabilityVisionLanguage)
		content := slices.Contains(reported.Capabilities, contentkit.CapabilityTextInterpret) ||
			slices.Contains(reported.Capabilities, contentkit.CapabilityAudioInterpret) ||
			slices.Contains(reported.Capabilities, contentkit.CapabilityImageInterpret) ||
			slices.Contains(reported.Capabilities, contentkit.CapabilityVideoInterpret) ||
			slices.Contains(reported.Capabilities, contentkit.CapabilityMultimodalInterpret)
		switch {
		case image && content:
			return &multiAdapter{base}, nil
		case image:
			return &imageAdapter{base}, nil
		case content:
			return &contentAdapter{base}, nil
		default:
			// Older image providers may advertise their own capability names.
			// They used contentkit.observe before the content contract existed.
			return &imageAdapter{base}, nil
		}
	}
	inspect := func(ctx context.Context, runtime contentkit.ProviderAdapterRuntime, model contentkit.InstalledModel) error {
		proc, err := launch.open(ctx, protocol, inspectPolicy())
		if err != nil {
			return err
		}
		defer proc.Close()
		return inspectModel(ctx, proc, runtime.Settings, model, protocol)
	}
	return registry.RegisterWithModelInspector(installed.Name(), factory, inspect)
}

// packagePath returns the verified location of a package-relative file that is
// itself an artifact, hashing just that artifact.
func packagePath(installed store.Installed, relative string) (string, error) {
	return installed.ArtifactPath(relative)
}

// packageFile returns the location of a package-relative file, which may be an
// artifact or a file inside a tree artifact such as a vendored "python"
// directory. It reads only the manifest and the file's metadata, so resolving a
// path inside a multi-gigabyte tree does not re-hash the tree. The caller must
// not run or trust the file before host.Open has verified the whole package,
// which it does before it starts anything.
func packageFile(installed store.Installed, relative string) (string, error) {
	clean := path.Clean(relative)
	if clean != relative || clean == "." || path.IsAbs(clean) || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("package path %q must be a clean relative path", relative)
	}
	for _, artifact := range installed.Manifest.Artifacts {
		covered := artifact.Path == clean
		if artifact.Kind == packagekit.ArtifactTree {
			covered = strings.HasPrefix(clean, artifact.Path+"/")
		}
		if !covered {
			continue
		}
		target := filepath.Join(installed.Directory, filepath.FromSlash(clean))
		info, err := os.Lstat(target)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("package path %q is not a regular file", relative)
		}
		return target, nil
	}
	return "", fmt.Errorf("package has no file %q", relative)
}

type launchPlan struct {
	installed  store.Installed
	descriptor *contentkit.ProviderPackage
}

func newLaunchPlan(installed store.Installed, protocol Protocol) (launchPlan, error) {
	plan := launchPlan{installed: installed}
	if installed.IsExecutable() {
		return plan, nil
	}
	// The package is verified in full when it is listed and again by host.Open
	// before anything starts, so registration does not hash it a third time.
	descriptorPath, err := packagePath(installed, protocol.DescriptorFile)
	if err != nil {
		return launchPlan{}, fmt.Errorf("source plugin descriptor: %w", err)
	}
	descriptor, err := contentkit.LoadProviderPackageWithAPIVersion(descriptorPath, protocol.DescriptorAPIVersion)
	if err != nil {
		return launchPlan{}, err
	}
	if _, err := packageFile(installed, descriptor.Entrypoint); err != nil {
		return launchPlan{}, fmt.Errorf("source plugin entrypoint: %w", err)
	}
	plan.descriptor = &descriptor
	return plan, nil
}

func (p launchPlan) open(ctx context.Context, protocol Protocol, authorize func(context.Context, plugin.Request) error) (*host.Process, error) {
	if p.descriptor == nil {
		return host.Open(ctx, p.installed, host.Options{Authorize: authorize})
	}
	root := protocol.RuntimeRoot
	if root == "" {
		var err error
		root, err = contentkit.DefaultRuntimeRoot()
		if err != nil {
			return nil, err
		}
	}
	runtime, err := contentkit.ResolveInstalledRuntime(root, p.descriptor.Runtime.Name, p.descriptor.Runtime.Version)
	if err != nil {
		return nil, fmt.Errorf("resolve Blockade runtime %s@%s: %w", p.descriptor.Runtime.Name, p.descriptor.Runtime.Version, err)
	}
	entrypoint, err := packageFile(p.installed, p.descriptor.Entrypoint)
	if err != nil {
		return nil, err
	}
	return host.Open(ctx, p.installed, host.Options{Authorize: authorize, Launch: &process.Command{
		Path: runtime.ExecutablePath(),
		Args: []string{"-B", entrypoint},
		Env: []string{
			"PYTHONNOUSERSITE=1",
			"PYTHONDONTWRITEBYTECODE=1",
			protocol.ManifestEnvironment + "=" + filepath.Join(p.installed.Directory, store.ManifestName),
		},
	}})
}

type processPool struct {
	processes []*host.Process
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
	var remote *plugin.RemoteError
	if !errors.As(err, &remote) {
		return err
	}
	kind := contentkit.ProviderAdapterErrorKind(remote.Code)
	switch kind {
	case contentkit.ProviderAdapterErrorAuthentication,
		contentkit.ProviderAdapterErrorRateLimit,
		contentkit.ProviderAdapterErrorTimeout,
		contentkit.ProviderAdapterErrorCanceled,
		contentkit.ProviderAdapterErrorUnavailable,
		contentkit.ProviderAdapterErrorInvalidRequest,
		contentkit.ProviderAdapterErrorInvalidResponse:
		mapped := contentkit.NewProviderAdapterError(kind, errors.New(remote.Message))
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
	runtime  contentkit.ProviderAdapterRuntime
	protocol Protocol
}

func (a *adapter) observe(ctx context.Context, request contentkit.ProviderAdapterObserveRequest) (contentkit.ProviderAdapterObserveResponse, error) {
	secrets := make(map[string]string, len(a.runtime.SecretNames))
	for _, name := range a.runtime.SecretNames {
		value, err := a.runtime.Secrets.Resolve(ctx, name)
		if err != nil {
			return contentkit.ProviderAdapterObserveResponse{}, err
		}
		secrets[name] = value
	}
	request.APIVersion = a.protocol.AdapterAPIVersion
	request.Request.APIVersion = a.protocol.ObservationRequestAPI
	var result contentkit.ProviderAdapterObserveResponse
	err := a.pool.Call(ctx, a.protocol.method("observe"), map[string]any{"request": request, "secrets": secrets}, &result)
	if err == nil {
		err = normalizeObserveResponse(&result, a.protocol)
	}
	return result, err
}
func (a *adapter) interpret(ctx context.Context, request contentkit.ProviderAdapterInterpretRequest) (contentkit.ProviderAdapterInterpretResponse, error) {
	secrets := make(map[string]string, len(a.runtime.SecretNames))
	for _, name := range a.runtime.SecretNames {
		value, err := a.runtime.Secrets.Resolve(ctx, name)
		if err != nil {
			return contentkit.ProviderAdapterInterpretResponse{}, err
		}
		secrets[name] = value
	}
	request.APIVersion = a.protocol.AdapterAPIVersion
	request.Request.APIVersion = a.protocol.ContentAPIVersion
	var result contentkit.ProviderAdapterInterpretResponse
	err := a.pool.Call(ctx, a.protocol.method("interpret"), map[string]any{"request": request, "secrets": secrets}, &result)
	if err == nil {
		err = normalizeInterpretResponse(&result, a.protocol)
	}
	return result, err
}

func inspectModel(ctx context.Context, proc *host.Process, settings map[string]string, model contentkit.InstalledModel, protocol Protocol) error {
	var result struct {
		APIVersion string `json:"apiVersion"`
		Compatible bool   `json:"compatible"`
		Detail     string `json:"detail,omitempty"`
	}
	if err := proc.Call(ctx, protocol.method("model.inspect"), map[string]any{"settings": settings, "model": wireModel(model, protocol)}, &result); err != nil {
		return err
	}
	if result.APIVersion != protocol.AdapterAPIVersion {
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidResponse, fmt.Errorf("model inspection returned unsupported apiVersion %q", result.APIVersion))
	}
	if !result.Compatible {
		if result.Detail == "" {
			return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, fmt.Errorf("model inspection rejected by provider"))
		}
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, fmt.Errorf("model inspection rejected by provider: %s", result.Detail))
	}
	return nil
}

type imageAdapter struct{ *adapter }

func (a *imageAdapter) Observe(ctx context.Context, request contentkit.ProviderAdapterObserveRequest) (contentkit.ProviderAdapterObserveResponse, error) {
	return a.observe(ctx, request)
}

type contentAdapter struct{ *adapter }

func (a *contentAdapter) Observe(context.Context, contentkit.ProviderAdapterObserveRequest) (contentkit.ProviderAdapterObserveResponse, error) {
	return contentkit.ProviderAdapterObserveResponse{}, contentkit.ErrModalityUnsupported
}

func (a *contentAdapter) Interpret(ctx context.Context, request contentkit.ProviderAdapterInterpretRequest) (contentkit.ProviderAdapterInterpretResponse, error) {
	return a.interpret(ctx, request)
}

type multiAdapter struct{ *adapter }

func (a *multiAdapter) Observe(ctx context.Context, request contentkit.ProviderAdapterObserveRequest) (contentkit.ProviderAdapterObserveResponse, error) {
	return a.observe(ctx, request)
}
func (a *multiAdapter) Interpret(ctx context.Context, request contentkit.ProviderAdapterInterpretRequest) (contentkit.ProviderAdapterInterpretResponse, error) {
	return a.interpret(ctx, request)
}
func (a *adapter) Capabilities(ctx context.Context) (contentkit.ProviderAdapterCapabilities, error) {
	var value contentkit.ProviderAdapterCapabilities
	err := a.pool.Call(ctx, a.protocol.method("capabilities"), nil, &value)
	if err == nil {
		err = normalizeAdapterVersion(&value.APIVersion, a.protocol)
	}
	return value, err
}
func (a *adapter) Health(ctx context.Context) (contentkit.ProviderAdapterHealth, error) {
	var value contentkit.ProviderAdapterHealth
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
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidResponse, fmt.Errorf("adapter returned unsupported apiVersion %q", *value))
	}
	*value = contentkit.ProviderAdapterAPIVersion
	return nil
}

func normalizeObserveResponse(result *contentkit.ProviderAdapterObserveResponse, protocol Protocol) error {
	if err := normalizeAdapterVersion(&result.APIVersion, protocol); err != nil {
		return err
	}
	if result.Response.APIVersion != protocol.ObservationAPIVersion {
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidResponse, fmt.Errorf("observation returned unsupported apiVersion %q", result.Response.APIVersion))
	}
	result.Response.APIVersion = contentkit.APIVersion
	return nil
}

func normalizeInterpretResponse(result *contentkit.ProviderAdapterInterpretResponse, protocol Protocol) error {
	if err := normalizeAdapterVersion(&result.APIVersion, protocol); err != nil {
		return err
	}
	if result.Response.APIVersion != protocol.ContentAPIVersion {
		return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidResponse, fmt.Errorf("content result returned unsupported apiVersion %q", result.Response.APIVersion))
	}
	result.Response.APIVersion = contentkit.ContentAPIVersion
	return nil
}

func wireModel(model contentkit.InstalledModel, protocol Protocol) map[string]any {
	return map[string]any{
		"manifest": map[string]any{
			"apiVersion": protocol.ModelAPIVersion,
			"name":       model.Manifest.Name, "version": model.Manifest.Version,
			"provider": model.Manifest.Provider, "capabilities": model.Manifest.Capabilities,
			"artifacts": model.Manifest.Artifacts,
		},
		"directory": model.Directory,
	}
}
