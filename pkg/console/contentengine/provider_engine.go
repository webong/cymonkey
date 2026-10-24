package contentengine

import (
	"context"
	"errors"
	"fmt"
	"jangolova/contentkit"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	defaultProviderAdapterTimeout         = 30 * time.Second
	maximumProviderAdapterTimeout         = 2 * time.Minute
	defaultProviderAdapterMaxPayloadBytes = int64(8 << 20)
	maximumProviderAdapterMaxPayloadBytes = int64(10 << 20)
)

type ProviderAdapterFactory func(context.Context, contentkit.ProviderAdapterRuntime) (contentkit.ProviderAdapter, error)

// ProviderModelInspectFunc validates a staged model without starting an
// inference runtime. Providers that accept local models register this callback
// beside their normal factory.
type ProviderModelInspectFunc func(context.Context, contentkit.ProviderAdapterRuntime, contentkit.InstalledModel) error

type SecretResolver interface {
	ResolveSecret(context.Context, string) (string, error)
}

type SecretResolverFunc func(context.Context, string) (string, error)

func (f SecretResolverFunc) ResolveSecret(ctx context.Context, name string) (string, error) {
	return f(ctx, name)
}

// EnvironmentSecretResolver resolves only environment references declared in
// ProviderAdapterConfig. It deliberately has no plaintext fallback.
type EnvironmentSecretResolver struct{}

func (EnvironmentSecretResolver) ResolveSecret(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return "", fmt.Errorf("environment variable %q is not set", name)
	}
	return value, nil
}

type configuredProviderAdapterSecrets struct {
	adapterID  string
	references map[string]SecretReference
	resolver   SecretResolver
}

func (s configuredProviderAdapterSecrets) Resolve(ctx context.Context, name string) (string, error) {
	reference, ok := s.references[name]
	if !ok {
		return "", contentkit.NewProviderAdapterErrorFor(s.adapterID, contentkit.ProviderAdapterErrorAuthentication, fmt.Errorf("secret %q is not configured", name))
	}
	value, err := s.resolver.ResolveSecret(ctx, reference.Env)
	if err != nil {
		return "", contentkit.NewProviderAdapterErrorFor(s.adapterID, contentkit.ProviderAdapterErrorAuthentication, err)
	}
	return value, nil
}

// ProviderAdapterRegistry maps installed provider names to factories.
// A Blockade build opts into real providers by registering their packages.
type ProviderAdapterRegistry struct {
	mu         sync.RWMutex
	factories  map[string]ProviderAdapterFactory
	inspectors map[string]ProviderModelInspectFunc
}

func NewProviderAdapterRegistry() *ProviderAdapterRegistry {
	return &ProviderAdapterRegistry{
		factories:  make(map[string]ProviderAdapterFactory),
		inspectors: make(map[string]ProviderModelInspectFunc),
	}
}

func (r *ProviderAdapterRegistry) Register(name string, factory ProviderAdapterFactory) error {
	return r.RegisterWithModelInspector(name, factory, nil)
}

// RegisterWithModelInspector registers provider startup and optional staged
// model inspection as separate lifecycle operations.
func (r *ProviderAdapterRegistry) RegisterWithModelInspector(name string, factory ProviderAdapterFactory, inspect ProviderModelInspectFunc) error {
	if r == nil {
		return errors.New("Blockade provider adapter registry is required")
	}
	name = canonicalProviderName(name)
	if !contentkit.ModelNamePattern.MatchString(name) {
		return errors.New("Blockade provider name is invalid")
	}
	if factory == nil {
		return fmt.Errorf("Blockade provider %q factory is required", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("Blockade provider %q is already registered", name)
	}
	r.factories[name] = factory
	if inspect != nil {
		r.inspectors[name] = inspect
	}
	return nil
}

func (r *ProviderAdapterRegistry) factory(name string) (ProviderAdapterFactory, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, ok := r.factories[canonicalProviderName(name)]
	return factory, ok
}

func (r *ProviderAdapterRegistry) inspector(name string) (ProviderModelInspectFunc, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	inspect, ok := r.inspectors[canonicalProviderName(name)]
	return inspect, ok
}

func canonicalProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

type providerAdapterEngine struct {
	mu              sync.RWMutex
	id              string
	adapter         contentkit.ProviderAdapter
	timeout         time.Duration
	maxPayloadBytes int64
}

func StartConfiguredProviderAdapter(ctx context.Context, config ProviderAdapterConfig, registry *ProviderAdapterRegistry, resolver SecretResolver) (Engine, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	factory, ok := registry.factory(config.Provider)
	if !ok {
		return nil, fmt.Errorf("Blockade provider %q uses unregistered package %q", config.ID, config.Provider)
	}
	if resolver == nil {
		resolver = EnvironmentSecretResolver{}
	}
	var model *contentkit.InstalledModel
	if config.Model != "" {
		root := config.ModelRoot
		if root == "" {
			var err error
			root, err = DefaultModelRoot()
			if err != nil {
				return nil, err
			}
		}
		installed, err := ResolveInstalledModel(root, canonicalProviderName(config.Provider), config.Model)
		if err != nil {
			return nil, fmt.Errorf("load model for provider %q: %w", config.Provider, err)
		}
		model = &installed
	}
	runtime := providerAdapterRuntime(config, resolver, model)
	adapter, err := factory(ctx, runtime)
	if err != nil {
		return nil, normalizeProviderAdapterError(config.ID, ctx, err)
	}
	if adapter == nil {
		return nil, contentkit.NewProviderAdapterErrorFor(config.ID, contentkit.ProviderAdapterErrorUnavailable, errors.New("factory returned nil adapter"))
	}
	return &providerAdapterEngine{
		id:              config.ID,
		adapter:         adapter,
		timeout:         config.timeout(),
		maxPayloadBytes: config.maxPayloadBytes(),
	}, nil
}

// InspectConfiguredProviderModel asks the selected provider to validate a
// staged model without constructing or starting its inference runtime.
func InspectConfiguredProviderModel(ctx context.Context, config ProviderAdapterConfig, model contentkit.InstalledModel, registry *ProviderAdapterRegistry, resolver SecretResolver) error {
	config.Model = ""
	config.ModelRoot = ""
	if err := config.validate(); err != nil {
		return err
	}
	if canonicalProviderName(model.Manifest.Provider) != canonicalProviderName(config.Provider) {
		return fmt.Errorf("model provider %q does not match configured provider %q", model.Manifest.Provider, config.Provider)
	}
	inspect, ok := registry.inspector(config.Provider)
	if !ok {
		return fmt.Errorf("Blockade provider %q cannot inspect local models", config.ID)
	}
	if resolver == nil {
		resolver = EnvironmentSecretResolver{}
	}
	callCtx, cancel := context.WithTimeout(ctx, config.timeout())
	defer cancel()
	if err := inspect(callCtx, providerAdapterRuntime(config, resolver, nil), model); err != nil {
		return normalizeProviderAdapterError(config.ID, callCtx, err)
	}
	if err := callCtx.Err(); err != nil {
		return normalizeProviderAdapterError(config.ID, callCtx, err)
	}
	return nil
}

func providerAdapterRuntime(config ProviderAdapterConfig, resolver SecretResolver, model *contentkit.InstalledModel) contentkit.ProviderAdapterRuntime {
	return contentkit.ProviderAdapterRuntime{
		ID:       config.ID,
		Workers:  config.workers(),
		Settings: maps.Clone(config.Settings),
		Model:    model,
		Secrets: configuredProviderAdapterSecrets{
			adapterID:  config.ID,
			references: maps.Clone(config.Secrets),
			resolver:   resolver,
		},
		SecretNames: slices.Sorted(maps.Keys(config.Secrets)),
	}
}

func (e *providerAdapterEngine) Observe(ctx context.Context, request contentkit.ObserveRequest) (contentkit.ObserveResponse, error) {
	if e == nil {
		return contentkit.ObserveResponse{}, contentkit.NewProviderAdapterErrorFor("", contentkit.ProviderAdapterErrorUnavailable, errors.New("adapter is closed"))
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.adapter == nil {
		return contentkit.ObserveResponse{}, contentkit.NewProviderAdapterErrorFor("", contentkit.ProviderAdapterErrorUnavailable, errors.New("adapter is closed"))
	}
	if request.APIVersion == "" {
		request.APIVersion = contentkit.APIVersion
	}
	if request.APIVersion != contentkit.APIVersion {
		return contentkit.ObserveResponse{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidRequest, fmt.Errorf("unsupported apiVersion %q", request.APIVersion))
	}
	if len(request.Image) == 0 {
		return contentkit.ObserveResponse{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidRequest, errors.New("image is required"))
	}
	if int64(len(request.Image))+int64(len(request.Prompt)) > e.maxPayloadBytes {
		return contentkit.ObserveResponse{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidRequest, fmt.Errorf("request exceeds %d-byte payload limit", e.maxPayloadBytes))
	}
	if request.RequestID == "" {
		request.RequestID = fmt.Sprintf("blockade-provider-%d", time.Now().UnixNano())
	}
	callCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	result, err := e.adapter.Observe(callCtx, contentkit.ProviderAdapterObserveRequest{
		APIVersion: contentkit.ProviderAdapterAPIVersion,
		Request:    request,
	})
	if err != nil {
		return contentkit.ObserveResponse{}, normalizeProviderAdapterError(e.id, callCtx, err)
	}
	if err := callCtx.Err(); err != nil {
		return contentkit.ObserveResponse{}, normalizeProviderAdapterError(e.id, callCtx, err)
	}
	if result.APIVersion != contentkit.ProviderAdapterAPIVersion {
		return contentkit.ObserveResponse{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidResponse, fmt.Errorf("unsupported adapter apiVersion %q", result.APIVersion))
	}
	if result.Response.RequestID != request.RequestID {
		return contentkit.ObserveResponse{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidResponse, fmt.Errorf("response requestId %q does not match %q", result.Response.RequestID, request.RequestID))
	}
	if err := contentkit.ValidateObserveResponse(result.Response); err != nil {
		return contentkit.ObserveResponse{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidResponse, err)
	}
	return result.Response, nil
}

func (e *providerAdapterEngine) Interpret(ctx context.Context, request contentkit.InterpretRequest) (contentkit.InterpretResponse, error) {
	if e == nil {
		return contentkit.InterpretResponse{}, contentkit.NewProviderAdapterErrorFor("", contentkit.ProviderAdapterErrorUnavailable, errors.New("adapter is closed"))
	}
	if request.APIVersion == "" {
		request.APIVersion = contentkit.ContentAPIVersion
	}
	if err := contentkit.ValidateInterpretRequest(request); err != nil {
		return contentkit.InterpretResponse{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidRequest, err)
	}
	size := len(request.Prompt)
	for _, part := range request.Parts {
		size += len(part.Text) + len(part.Data)
	}
	if int64(size) > e.maxPayloadBytes {
		return contentkit.InterpretResponse{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidRequest, fmt.Errorf("request exceeds %d-byte payload limit", e.maxPayloadBytes))
	}
	if request.RequestID == "" {
		request.RequestID = fmt.Sprintf("blockade-content-%d", time.Now().UnixNano())
	}
	capabilities, err := e.Capabilities(ctx)
	if err != nil {
		return contentkit.InterpretResponse{}, err
	}
	required := contentkit.RequiredContentCapability(request.Parts)
	if !slices.Contains(capabilities, required) && !slices.Contains(capabilities, contentkit.CapabilityMultimodalInterpret) {
		return contentkit.InterpretResponse{}, contentkit.ErrModalityUnsupported
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.adapter == nil {
		return contentkit.InterpretResponse{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorUnavailable, errors.New("adapter is closed"))
	}
	adapter, ok := e.adapter.(contentkit.ContentProviderAdapter)
	if !ok {
		return contentkit.InterpretResponse{}, contentkit.ErrModalityUnsupported
	}
	callCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	result, err := adapter.Interpret(callCtx, contentkit.ProviderAdapterInterpretRequest{
		APIVersion: contentkit.ProviderAdapterAPIVersion,
		Request:    request,
	})
	if err != nil {
		return contentkit.InterpretResponse{}, normalizeProviderAdapterError(e.id, callCtx, err)
	}
	if err := callCtx.Err(); err != nil {
		return contentkit.InterpretResponse{}, normalizeProviderAdapterError(e.id, callCtx, err)
	}
	if result.APIVersion != contentkit.ProviderAdapterAPIVersion {
		return contentkit.InterpretResponse{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidResponse, fmt.Errorf("unsupported adapter apiVersion %q", result.APIVersion))
	}
	if err := contentkit.ValidateInterpretResponse(result.Response, request); err != nil {
		return contentkit.InterpretResponse{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidResponse, err)
	}
	return result.Response, nil
}

func (e *providerAdapterEngine) Capabilities(ctx context.Context) ([]string, error) {
	if e == nil {
		return nil, contentkit.NewProviderAdapterErrorFor("", contentkit.ProviderAdapterErrorUnavailable, errors.New("adapter is closed"))
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.adapter == nil {
		return nil, contentkit.NewProviderAdapterErrorFor("", contentkit.ProviderAdapterErrorUnavailable, errors.New("adapter is closed"))
	}
	callCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	result, err := e.adapter.Capabilities(callCtx)
	if err != nil {
		return nil, normalizeProviderAdapterError(e.id, callCtx, err)
	}
	if err := callCtx.Err(); err != nil {
		return nil, normalizeProviderAdapterError(e.id, callCtx, err)
	}
	if result.APIVersion != contentkit.ProviderAdapterAPIVersion {
		return nil, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidResponse, fmt.Errorf("unsupported capabilities apiVersion %q", result.APIVersion))
	}
	capabilities := append([]string(nil), result.Capabilities...)
	// Existing image-only adapters inherited image.observe before the content
	// contract existed. Preserve that behavior without assigning it to a
	// content-only adapter.
	if _, content := e.adapter.(contentkit.ContentProviderAdapter); !content || slices.Contains(capabilities, contentkit.CapabilityImageObserve) {
		capabilities = append(capabilities, contentkit.CapabilityImageObserve)
	}
	for i := range capabilities {
		capabilities[i] = strings.TrimSpace(capabilities[i])
		if capabilities[i] == "" {
			return nil, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidResponse, errors.New("adapter returned an empty capability"))
		}
	}
	slices.Sort(capabilities)
	return slices.Compact(capabilities), nil
}

func (e *providerAdapterEngine) Health(ctx context.Context) (EngineHealth, error) {
	if e == nil {
		return EngineHealth{Ready: false}, nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.adapter == nil {
		return EngineHealth{Ready: false}, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	health, err := e.adapter.Health(callCtx)
	if err != nil {
		return EngineHealth{}, normalizeProviderAdapterError(e.id, callCtx, err)
	}
	if err := callCtx.Err(); err != nil {
		return EngineHealth{}, normalizeProviderAdapterError(e.id, callCtx, err)
	}
	if health.APIVersion != contentkit.ProviderAdapterAPIVersion {
		return EngineHealth{}, contentkit.NewProviderAdapterErrorFor(e.id, contentkit.ProviderAdapterErrorInvalidResponse, fmt.Errorf("unsupported health apiVersion %q", health.APIVersion))
	}
	return EngineHealth{Ready: health.Ready, Detail: health.Detail}, nil
}

func (e *providerAdapterEngine) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.adapter == nil {
		return nil
	}
	err := e.adapter.Close()
	e.adapter = nil
	return err
}

func normalizeProviderAdapterError(adapterID string, ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var adapterErr *contentkit.ProviderAdapterError
	if errors.As(err, &adapterErr) {
		copy := *adapterErr
		if copy.AdapterID == "" {
			copy.AdapterID = adapterID
		}
		return &copy
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return contentkit.NewProviderAdapterErrorFor(adapterID, contentkit.ProviderAdapterErrorTimeout, err)
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return contentkit.NewProviderAdapterErrorFor(adapterID, contentkit.ProviderAdapterErrorCanceled, err)
	}
	return contentkit.NewProviderAdapterErrorFor(adapterID, contentkit.ProviderAdapterErrorUnavailable, err)
}
