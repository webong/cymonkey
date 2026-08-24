package grimlock

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"jangolova/internal/blockade"
)

// VisionProvider is the Grimlock-owned integration boundary for non-language
// and multimodal vision providers. Blockade supplies the normalized request
// and response types; provider-specific authentication and payload mapping
// stay on the Grimlock side.
type VisionProvider interface {
	Protocol() string
	Observe(context.Context, blockade.ObserveRequest) (blockade.ObserveResponse, error)
}

type VisionProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]VisionProvider
}

func NewVisionProviderRegistry(values ...VisionProvider) (*VisionProviderRegistry, error) {
	r := &VisionProviderRegistry{providers: make(map[string]VisionProvider)}
	for _, provider := range values {
		if err := r.Register(provider); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *VisionProviderRegistry) Register(provider VisionProvider) error {
	if provider == nil {
		return errors.New("Grimlock vision provider is required")
	}
	protocol := strings.TrimSpace(provider.Protocol())
	if !protocolPattern.MatchString(protocol) {
		return fmt.Errorf("Grimlock vision provider protocol %q is invalid", protocol)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers == nil {
		r.providers = make(map[string]VisionProvider)
	}
	if _, exists := r.providers[protocol]; exists {
		return fmt.Errorf("Grimlock vision provider %q is already registered", protocol)
	}
	r.providers[protocol] = provider
	return nil
}

func (r *VisionProviderRegistry) Provider(protocol string) (VisionProvider, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[protocol]
	return provider, ok
}

func (r *VisionProviderRegistry) Protocols() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make([]string, 0, len(r.providers))
	for protocol := range r.providers {
		values = append(values, protocol)
	}
	sort.Strings(values)
	return values
}
