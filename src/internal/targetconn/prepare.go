package targetconn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cymonkey/src/internal/orchestrator"
)

const minimumCredentialValidity = 5 * time.Second

type RenewalOptions struct {
	RenewBefore    time.Duration
	RetryInterval  time.Duration
	ResolveTimeout time.Duration
}

var defaultRenewalOptions = RenewalOptions{
	RenewBefore: 30 * time.Second, RetryInterval: 2 * time.Second, ResolveTimeout: 10 * time.Second,
}

// Prepare resolves all references into a cloned target. Release must be called
// after the adapter disconnects, or immediately if connection fails.
func Prepare(ctx context.Context, resolver Resolver, target orchestrator.EngineTarget) (orchestrator.EngineTarget, func(context.Context) error, error) {
	return PrepareWithOptions(ctx, resolver, target, defaultRenewalOptions)
}

// PrepareWithOptions exists for deterministic embedders and conformance tests.
// Production callers should normally use Prepare's conservative defaults.
func PrepareWithOptions(ctx context.Context, resolver Resolver, target orchestrator.EngineTarget, options RenewalOptions) (orchestrator.EngineTarget, func(context.Context) error, error) {
	options = normalizeRenewalOptions(options)
	prepared := cloneTarget(target)
	var materials []*orchestrator.EndpointConnection
	var leases []*materialLease
	var releaseOnce sync.Once
	var releaseErr error
	release := func(releaseCtx context.Context) error {
		releaseOnce.Do(func() {
			var problems []error
			for index := len(leases) - 1; index >= 0; index-- {
				if err := leases[index].close(releaseCtx); err != nil {
					problems = append(problems, err)
				}
			}
			for _, material := range materials {
				material.Clear()
			}
			releaseErr = errors.Join(problems...)
		})
		return releaseErr
	}
	for index := range prepared.Endpoints {
		endpoint := &prepared.Endpoints[index]
		if endpoint.CredentialRef == "" && endpoint.TLSRef == "" {
			continue
		}
		if resolver == nil {
			_ = release(context.Background())
			return orchestrator.EngineTarget{}, func(context.Context) error { return nil }, errors.New("target connection material resolver is not configured")
		}
		connection := &orchestrator.EndpointConnection{}
		for _, item := range []struct {
			kind      ReferenceKind
			reference string
		}{{CredentialReference, endpoint.CredentialRef}, {TLSReference, endpoint.TLSRef}} {
			if item.reference == "" {
				continue
			}
			material, err := resolver.Resolve(ctx, Request{
				Reference: item.reference, Kind: item.kind, TargetID: target.TargetID,
				EndpointName: endpoint.Name, Protocol: endpoint.Protocol, Audience: endpoint.Audience,
			})
			if err != nil {
				_ = release(context.Background())
				return orchestrator.EngineTarget{}, func(context.Context) error { return nil }, referenceError(item.kind, item.reference, err)
			}
			if err := validateResolvedMaterial(item.kind, material); err != nil {
				if material.Release != nil {
					_ = material.Release(context.Background())
				}
				_ = release(context.Background())
				return orchestrator.EngineTarget{}, func(context.Context) error { return nil }, referenceError(item.kind, item.reference, err)
			}
			lease := newMaterialLease(resolver, Request{
				Reference: item.reference, Kind: item.kind, TargetID: target.TargetID,
				EndpointName: endpoint.Name, Protocol: endpoint.Protocol, Audience: endpoint.Audience,
			}, material, connection, options)
			lease.apply(material)
			leases = append(leases, lease)
			if !material.ExpiresAt.IsZero() {
				lease.start()
			}
		}
		endpoint.Connection = connection
		materials = append(materials, connection)
	}
	return prepared, release, nil
}

type materialLease struct {
	mu         sync.Mutex
	resolver   Resolver
	request    Request
	current    Material
	connection *orchestrator.EndpointConnection
	options    RenewalOptions
	stop       chan struct{}
	closed     bool
	startOnce  sync.Once
	pending    map[uint64]Material
}

func newMaterialLease(resolver Resolver, request Request, material Material, connection *orchestrator.EndpointConnection, options RenewalOptions) *materialLease {
	return &materialLease{
		resolver: resolver, request: request, current: material, connection: connection,
		options: options, stop: make(chan struct{}), pending: make(map[uint64]Material),
	}
}

func (l *materialLease) apply(material Material) uint64 {
	if l.request.Kind == CredentialReference {
		return l.connection.ReplaceCredential(cloneHeaders(material.Headers), material.ExpiresAt)
	}
	return l.connection.ReplaceTLS(material.TLS, material.ExpiresAt)
}

func (l *materialLease) start() {
	l.startOnce.Do(func() { go l.run() })
}

func (l *materialLease) run() {
	for {
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			return
		}
		expiresAt := l.current.ExpiresAt
		l.mu.Unlock()
		delay := time.Until(expiresAt) - l.options.RenewBefore
		if delay < 100*time.Millisecond {
			delay = 100 * time.Millisecond
		}
		if !waitForLease(delay, l.stop) {
			return
		}
		if l.renew() {
			continue
		}
		if !waitForLease(l.options.RetryInterval, l.stop) {
			return
		}
	}
}

func (l *materialLease) renew() bool {
	ctx, cancel := context.WithTimeout(context.Background(), l.options.ResolveTimeout)
	material, err := l.resolver.Resolve(ctx, l.request)
	cancel()
	if err != nil || validateResolvedMaterial(l.request.Kind, material) != nil {
		if err == nil {
			releaseMaterial(context.Background(), material)
		}
		return false
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		releaseMaterial(context.Background(), material)
		return false
	}
	if !material.ExpiresAt.After(l.current.ExpiresAt) {
		l.mu.Unlock()
		releaseMaterial(context.Background(), material)
		return false
	}
	previous := l.current
	l.current = material
	revision := l.apply(material)
	if requiresConnectionAcknowledgement(l.request.Kind, l.request.Protocol) {
		l.pending[revision] = previous
	}
	l.mu.Unlock()
	if requiresConnectionAcknowledgement(l.request.Kind, l.request.Protocol) {
		l.waitForAcknowledgement(revision, previous.ExpiresAt)
		l.releasePending(revision)
	} else {
		releaseMaterial(context.Background(), previous)
	}
	return true
}

func (l *materialLease) close(ctx context.Context) error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	close(l.stop)
	material := l.current
	l.current = Material{}
	values := make([]Material, 0, len(l.pending)+1)
	values = append(values, material)
	for _, pending := range l.pending {
		values = append(values, pending)
	}
	l.pending = nil
	l.mu.Unlock()
	var problems []error
	for _, value := range values {
		if err := releaseMaterial(ctx, value); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

func (l *materialLease) waitForAcknowledgement(revision uint64, oldExpiry time.Time) {
	acknowledgements, acknowledged := l.connection.Acknowledgements()
	if acknowledged >= revision {
		return
	}
	delay := time.Until(oldExpiry)
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case value := <-acknowledgements:
			if value >= revision {
				return
			}
		case <-timer.C:
			return
		case <-l.stop:
			return
		}
	}
}

func (l *materialLease) releasePending(revision uint64) {
	l.mu.Lock()
	material, ok := l.pending[revision]
	if ok {
		delete(l.pending, revision)
	}
	l.mu.Unlock()
	if ok {
		releaseMaterial(context.Background(), material)
	}
}

func requiresConnectionAcknowledgement(kind ReferenceKind, protocol string) bool {
	if kind == TLSReference {
		return true
	}
	switch protocol {
	case "cdp", "webdriver-bidi", "websocket":
		return true
	default:
		return false
	}
}

func releaseMaterial(ctx context.Context, material Material) error {
	var err error
	if material.Release != nil {
		if releaseErr := material.Release(ctx); releaseErr != nil {
			err = errors.New("release connection material")
		}
	}
	wipeHeaders(material.Headers)
	return err
}

func waitForLease(delay time.Duration, stop <-chan struct{}) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-stop:
		return false
	}
}

func normalizeRenewalOptions(options RenewalOptions) RenewalOptions {
	if options.RenewBefore <= 0 {
		options.RenewBefore = defaultRenewalOptions.RenewBefore
	}
	if options.RetryInterval <= 0 {
		options.RetryInterval = defaultRenewalOptions.RetryInterval
	}
	if options.ResolveTimeout <= 0 {
		options.ResolveTimeout = defaultRenewalOptions.ResolveTimeout
	}
	return options
}

func validateResolvedMaterial(kind ReferenceKind, material Material) error {
	switch kind {
	case CredentialReference:
		if len(material.Headers) == 0 || material.TLS != nil {
			return errors.New("credential material must contain headers only")
		}
		if err := validateHeaders(material.Headers); err != nil {
			return err
		}
		if material.ExpiresAt.IsZero() || !material.ExpiresAt.After(time.Now().Add(minimumCredentialValidity)) {
			return errors.New("connection material is expired or expires too soon")
		}
	case TLSReference:
		if len(material.Headers) != 0 || material.TLS == nil {
			return errors.New("TLS material must contain tls only")
		}
		_, err := validateTLS(tlsDocument{
			CAFile: material.TLS.CAFile, ClientCertificateFile: material.TLS.ClientCertificateFile,
			ClientKeyFile: material.TLS.ClientKeyFile, ServerName: material.TLS.ServerName,
		})
		if err != nil {
			return err
		}
		if !material.ExpiresAt.IsZero() && !material.ExpiresAt.After(time.Now().Add(minimumCredentialValidity)) {
			return errors.New("connection material is expired or expires too soon")
		}
	default:
		return errors.New("unsupported connection material reference kind")
	}
	return nil
}

func Validate(endpoint orchestrator.TargetEndpoint) error {
	snapshot := endpoint.Connection.Snapshot()
	if !snapshot.ExpiresAt.IsZero() && !snapshot.ExpiresAt.After(time.Now()) {
		return errors.New("target connection material has expired")
	}
	return nil
}

func cloneTarget(target orchestrator.EngineTarget) orchestrator.EngineTarget {
	result := target
	result.Endpoints = append([]orchestrator.TargetEndpoint(nil), target.Endpoints...)
	result.Handles = make(orchestrator.EngineHandles, len(target.Handles))
	for name, value := range target.Handles {
		result.Handles[name] = value
	}
	result.Metadata = cloneValues(target.Metadata)
	for index := range result.Endpoints {
		result.Endpoints[index].Metadata = cloneValues(result.Endpoints[index].Metadata)
		result.Endpoints[index].Connection = nil
	}
	return result
}

func cloneValues(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for name, value := range values {
		result[name] = value
	}
	return result
}

func referenceError(kind ReferenceKind, reference string, err error) error {
	switch {
	case errors.Is(err, ErrReferenceNotFound):
		return fmt.Errorf("resolve %s reference %q: reference was not found", kind, reference)
	case strings.Contains(err.Error(), "expired"):
		return fmt.Errorf("resolve %s reference %q: connection material is expired or expires too soon", kind, reference)
	default:
		return fmt.Errorf("resolve %s reference %q: connection material is unavailable", kind, reference)
	}
}
