package blockade

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	ProviderAdapterAPIVersion = "blockade.provider-adapter/v1alpha1"

	CapabilityImageObserve  = "image.observe"
	CapabilityObjectDetect  = "object.detect"
	CapabilityObjectSegment = "object.segment"

	defaultProviderAdapterTimeout         = 30 * time.Second
	maximumProviderAdapterTimeout         = 2 * time.Minute
	defaultProviderAdapterMaxPayloadBytes = int64(8 << 20)
	maximumProviderAdapterMaxPayloadBytes = int64(10 << 20)
)

// ProviderAdapterObserveRequest is the versioned boundary between Blockade
// and a hosted vision/VLM adapter. The nested request remains the public
// blockade.observation/v1alpha1 request consumed by every inference backend.
type ProviderAdapterObserveRequest struct {
	APIVersion string         `json:"apiVersion"`
	Request    ObserveRequest `json:"request"`
}

// ProviderAdapterObserveResponse wraps the normalized Blockade response so
// the adapter protocol can evolve independently from the observation schema.
type ProviderAdapterObserveResponse struct {
	APIVersion string          `json:"apiVersion"`
	Response   ObserveResponse `json:"response"`
}

type ProviderAdapterCapabilities struct {
	APIVersion   string   `json:"apiVersion"`
	Capabilities []string `json:"capabilities"`
}

type ProviderAdapterHealth struct {
	APIVersion string `json:"apiVersion"`
	Ready      bool   `json:"ready"`
	Detail     string `json:"detail,omitempty"`
}

// ProviderAdapter is implemented by Blockade-owned provider integration
// packages. Implementations keep provider-native requests and responses behind
// this interface, return only normalized observation data, and must support
// concurrent method calls until Close begins.
type ProviderAdapter interface {
	Observe(context.Context, ProviderAdapterObserveRequest) (ProviderAdapterObserveResponse, error)
	Capabilities(context.Context) (ProviderAdapterCapabilities, error)
	Health(context.Context) (ProviderAdapterHealth, error)
	Close() error
}

type ProviderAdapterFactory func(context.Context, ProviderAdapterRuntime) (ProviderAdapter, error)

// ProviderAdapterRuntime contains non-secret settings and a lazy secret source.
// Secret values never appear in the YAML-backed configuration value.
type ProviderAdapterRuntime struct {
	ID       string
	Settings map[string]string
	Secrets  ProviderAdapterSecrets
}

type ProviderAdapterSecrets interface {
	Resolve(context.Context, string) (string, error)
}

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
		return "", newProviderAdapterError(s.adapterID, ProviderAdapterErrorAuthentication, fmt.Errorf("secret %q is not configured", name))
	}
	value, err := s.resolver.ResolveSecret(ctx, reference.Env)
	if err != nil {
		return "", newProviderAdapterError(s.adapterID, ProviderAdapterErrorAuthentication, err)
	}
	return value, nil
}

// ProviderAdapterRegistry maps provider-neutral adapter kinds to factories.
// A Blockade build opts into real providers by registering their packages.
type ProviderAdapterRegistry struct {
	mu        sync.RWMutex
	factories map[string]ProviderAdapterFactory
}

func NewProviderAdapterRegistry() *ProviderAdapterRegistry {
	return &ProviderAdapterRegistry{factories: make(map[string]ProviderAdapterFactory)}
}

func (r *ProviderAdapterRegistry) Register(kind string, factory ProviderAdapterFactory) error {
	if r == nil {
		return errors.New("Blockade provider adapter registry is required")
	}
	kind = canonicalAdapterKind(kind)
	if kind == "" {
		return errors.New("Blockade provider adapter kind is required")
	}
	if factory == nil {
		return fmt.Errorf("Blockade provider adapter %q factory is required", kind)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[kind]; exists {
		return fmt.Errorf("Blockade provider adapter kind %q is already registered", kind)
	}
	r.factories[kind] = factory
	return nil
}

func (r *ProviderAdapterRegistry) factory(kind string) (ProviderAdapterFactory, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, ok := r.factories[canonicalAdapterKind(kind)]
	return factory, ok
}

func canonicalAdapterKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

type ProviderAdapterErrorKind string

const (
	ProviderAdapterErrorAuthentication  ProviderAdapterErrorKind = "authentication"
	ProviderAdapterErrorRateLimit       ProviderAdapterErrorKind = "rate_limit"
	ProviderAdapterErrorTimeout         ProviderAdapterErrorKind = "timeout"
	ProviderAdapterErrorCanceled        ProviderAdapterErrorKind = "canceled"
	ProviderAdapterErrorUnavailable     ProviderAdapterErrorKind = "unavailable"
	ProviderAdapterErrorInvalidRequest  ProviderAdapterErrorKind = "invalid_request"
	ProviderAdapterErrorInvalidResponse ProviderAdapterErrorKind = "invalid_response"
)

// ProviderAdapterError preserves machine-readable failure categories without
// exposing provider responses or secret values through its public message.
type ProviderAdapterError struct {
	Kind       ProviderAdapterErrorKind
	AdapterID  string
	RetryAfter time.Duration
	cause      error
}

func NewProviderAdapterError(kind ProviderAdapterErrorKind, cause error) *ProviderAdapterError {
	return newProviderAdapterError("", kind, cause)
}

func newProviderAdapterError(adapterID string, kind ProviderAdapterErrorKind, cause error) *ProviderAdapterError {
	return &ProviderAdapterError{Kind: kind, AdapterID: adapterID, cause: cause}
}

func (e *ProviderAdapterError) Error() string {
	if e == nil {
		return "Blockade provider adapter failed"
	}
	if e.AdapterID == "" {
		return fmt.Sprintf("Blockade provider adapter failed: %s", e.Kind)
	}
	return fmt.Sprintf("Blockade provider adapter %q failed: %s", e.AdapterID, e.Kind)
}

func (e *ProviderAdapterError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func ProviderAdapterErrorKindOf(err error) (ProviderAdapterErrorKind, bool) {
	var adapterErr *ProviderAdapterError
	if !errors.As(err, &adapterErr) {
		return "", false
	}
	return adapterErr.Kind, true
}

func validProviderAdapterErrorKind(kind ProviderAdapterErrorKind) bool {
	switch kind {
	case ProviderAdapterErrorAuthentication,
		ProviderAdapterErrorRateLimit,
		ProviderAdapterErrorTimeout,
		ProviderAdapterErrorCanceled,
		ProviderAdapterErrorUnavailable,
		ProviderAdapterErrorInvalidRequest,
		ProviderAdapterErrorInvalidResponse:
		return true
	default:
		return false
	}
}

type providerAdapterEngine struct {
	mu              sync.RWMutex
	id              string
	adapter         ProviderAdapter
	timeout         time.Duration
	maxPayloadBytes int64
}

func StartConfiguredProviderAdapter(ctx context.Context, config ProviderAdapterConfig, registry *ProviderAdapterRegistry, resolver SecretResolver) (Engine, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	factory, ok := registry.factory(config.Kind)
	if !ok {
		return nil, fmt.Errorf("Blockade provider adapter %q has unregistered kind %q", config.ID, config.Kind)
	}
	if resolver == nil {
		resolver = EnvironmentSecretResolver{}
	}
	runtime := ProviderAdapterRuntime{
		ID:       config.ID,
		Settings: maps.Clone(config.Settings),
		Secrets: configuredProviderAdapterSecrets{
			adapterID:  config.ID,
			references: maps.Clone(config.Secrets),
			resolver:   resolver,
		},
	}
	adapter, err := factory(ctx, runtime)
	if err != nil {
		return nil, normalizeProviderAdapterError(config.ID, ctx, err)
	}
	if adapter == nil {
		return nil, newProviderAdapterError(config.ID, ProviderAdapterErrorUnavailable, errors.New("factory returned nil adapter"))
	}
	return &providerAdapterEngine{
		id:              config.ID,
		adapter:         adapter,
		timeout:         config.timeout(),
		maxPayloadBytes: config.maxPayloadBytes(),
	}, nil
}

func (e *providerAdapterEngine) Observe(ctx context.Context, request ObserveRequest) (ObserveResponse, error) {
	if e == nil {
		return ObserveResponse{}, newProviderAdapterError("", ProviderAdapterErrorUnavailable, errors.New("adapter is closed"))
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.adapter == nil {
		return ObserveResponse{}, newProviderAdapterError("", ProviderAdapterErrorUnavailable, errors.New("adapter is closed"))
	}
	if request.APIVersion == "" {
		request.APIVersion = APIVersion
	}
	if request.APIVersion != APIVersion {
		return ObserveResponse{}, newProviderAdapterError(e.id, ProviderAdapterErrorInvalidRequest, fmt.Errorf("unsupported apiVersion %q", request.APIVersion))
	}
	if len(request.Image) == 0 {
		return ObserveResponse{}, newProviderAdapterError(e.id, ProviderAdapterErrorInvalidRequest, errors.New("image is required"))
	}
	if int64(len(request.Image))+int64(len(request.Prompt)) > e.maxPayloadBytes {
		return ObserveResponse{}, newProviderAdapterError(e.id, ProviderAdapterErrorInvalidRequest, fmt.Errorf("request exceeds %d-byte payload limit", e.maxPayloadBytes))
	}
	if request.RequestID == "" {
		request.RequestID = fmt.Sprintf("blockade-provider-%d", time.Now().UnixNano())
	}
	callCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	result, err := e.adapter.Observe(callCtx, ProviderAdapterObserveRequest{
		APIVersion: ProviderAdapterAPIVersion,
		Request:    request,
	})
	if err != nil {
		return ObserveResponse{}, normalizeProviderAdapterError(e.id, callCtx, err)
	}
	if err := callCtx.Err(); err != nil {
		return ObserveResponse{}, normalizeProviderAdapterError(e.id, callCtx, err)
	}
	if result.APIVersion != ProviderAdapterAPIVersion {
		return ObserveResponse{}, newProviderAdapterError(e.id, ProviderAdapterErrorInvalidResponse, fmt.Errorf("unsupported adapter apiVersion %q", result.APIVersion))
	}
	if result.Response.RequestID != request.RequestID {
		return ObserveResponse{}, newProviderAdapterError(e.id, ProviderAdapterErrorInvalidResponse, fmt.Errorf("response requestId %q does not match %q", result.Response.RequestID, request.RequestID))
	}
	if err := ValidateObserveResponse(result.Response); err != nil {
		return ObserveResponse{}, newProviderAdapterError(e.id, ProviderAdapterErrorInvalidResponse, err)
	}
	return result.Response, nil
}

func (e *providerAdapterEngine) Capabilities(ctx context.Context) ([]string, error) {
	if e == nil {
		return nil, newProviderAdapterError("", ProviderAdapterErrorUnavailable, errors.New("adapter is closed"))
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.adapter == nil {
		return nil, newProviderAdapterError("", ProviderAdapterErrorUnavailable, errors.New("adapter is closed"))
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
	if result.APIVersion != ProviderAdapterAPIVersion {
		return nil, newProviderAdapterError(e.id, ProviderAdapterErrorInvalidResponse, fmt.Errorf("unsupported capabilities apiVersion %q", result.APIVersion))
	}
	capabilities := append([]string{CapabilityImageObserve}, result.Capabilities...)
	for i := range capabilities {
		capabilities[i] = strings.TrimSpace(capabilities[i])
		if capabilities[i] == "" {
			return nil, newProviderAdapterError(e.id, ProviderAdapterErrorInvalidResponse, errors.New("adapter returned an empty capability"))
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
	if health.APIVersion != ProviderAdapterAPIVersion {
		return EngineHealth{}, newProviderAdapterError(e.id, ProviderAdapterErrorInvalidResponse, fmt.Errorf("unsupported health apiVersion %q", health.APIVersion))
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
	var adapterErr *ProviderAdapterError
	if errors.As(err, &adapterErr) {
		copy := *adapterErr
		if copy.AdapterID == "" {
			copy.AdapterID = adapterID
		}
		return &copy
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return newProviderAdapterError(adapterID, ProviderAdapterErrorTimeout, err)
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return newProviderAdapterError(adapterID, ProviderAdapterErrorCanceled, err)
	}
	return newProviderAdapterError(adapterID, ProviderAdapterErrorUnavailable, err)
}
