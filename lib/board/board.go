// Package board defines a host-independent boundary for input and output
// devices. Providers bind concrete devices to this contract; the calling host
// decides which device and capabilities a session may use.
package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Capability names describe one bounded operation. Providers may add names
// outside this initial vocabulary when they document their input and output.
type Capability string

const (
	KeyboardPress Capability = "keyboard.press"
	KeyboardType  Capability = "keyboard.type"
	DriveList     Capability = "drive.list"
	DriveRead     Capability = "drive.read"
)

// CapabilityDescriptor gives a host the action schema it needs to advertise
// a provider capability. The provider validates inputs against its schema.
type CapabilityDescriptor struct {
	Name         Capability      `json:"name"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

// Device is a provider-owned device available for an explicit host grant.
// IDs are stable only within the provider that reports them.
type Device struct {
	ProviderID   string                 `json:"providerId"`
	ID           string                 `json:"id"`
	Kind         string                 `json:"kind"`
	Name         string                 `json:"name,omitempty"`
	Capabilities []CapabilityDescriptor `json:"capabilities"`
}

// Grant is supplied by the calling host for one device session. ResourceIDs
// are opaque roots or handles. Board requires each drive action to name one
// granted root; the provider must confine listing and reading beneath it,
// including paths reached through links or aliases.
type Grant struct {
	Capabilities []Capability `json:"capabilities"`
	ResourceIDs  []string     `json:"resourceIds,omitempty"`
}

type OpenRequest struct {
	ProviderID string `json:"providerId"`
	DeviceID   string `json:"deviceId"`
	Grant      Grant  `json:"grant"`
}

// Action uses a capability name so hosts can bind Board sessions to their own
// protocol. Providers validate the capability-specific input schema.
type Action struct {
	Capability Capability      `json:"capability"`
	ResourceID string          `json:"resourceId,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
}

// Result carries one small structured answer. It is JSON so a host can log,
// compare, or forward a result without special-casing device output. Resource
// bytes do not belong here; a provider delivers those through Stream.
type Result struct {
	Output json.RawMessage `json:"output,omitempty"`
}

// Provider implements discovery and attachment for one platform or transport.
// Open must apply the grant itself; Board's gate checks capability names while
// only the provider can enforce device and resource boundaries.
type Provider interface {
	ID() string
	List(context.Context) ([]Device, error)
	Open(context.Context, string, Grant) (Session, error)
}

// Session releases only the Board attachment when closed. The provider or
// calling host continues to own the underlying device and its lifecycle.
type Session interface {
	Invoke(context.Context, Action) (Result, error)
	Close(context.Context) error
}

var (
	ErrClosed          = errors.New("board session is closed")
	ErrNotGranted      = errors.New("board capability or resource is not granted")
	ErrUnknownProvider = errors.New("board provider is not registered")
	ErrUnknownDevice   = errors.New("board device is not registered")
)

type Registry struct {
	providers map[string]Provider
}

func NewRegistry(providers ...Provider) (*Registry, error) {
	r := &Registry{providers: make(map[string]Provider, len(providers))}
	for _, provider := range providers {
		if provider == nil {
			return nil, errors.New("board provider needs a nonempty ID")
		}
		id := provider.ID()
		if strings.TrimSpace(id) == "" {
			return nil, errors.New("board provider needs a nonempty ID")
		}
		if _, exists := r.providers[id]; exists {
			return nil, fmt.Errorf("duplicate board provider %q", id)
		}
		r.providers[id] = provider
	}
	return r, nil
}

// List returns only explicitly registered providers' devices. A caller may
// choose to filter this result before presenting it to an agent.
func (r *Registry) List(ctx context.Context) ([]Device, error) {
	if r == nil {
		return nil, errors.New("board registry is nil")
	}
	var devices []Device
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		provider := r.providers[id]
		listed, err := provider.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list board provider %q: %w", id, err)
		}
		seen := make(map[string]struct{}, len(listed))
		for _, device := range listed {
			if strings.TrimSpace(device.ID) == "" || strings.TrimSpace(device.Kind) == "" {
				return nil, fmt.Errorf("board provider %q reported an invalid device", id)
			}
			if _, exists := seen[device.ID]; exists {
				return nil, fmt.Errorf("board provider %q reported duplicate device %q", id, device.ID)
			}
			if err := validateCapabilities(device.Capabilities); err != nil {
				return nil, fmt.Errorf("board provider %q device %q: %w", id, device.ID, err)
			}
			seen[device.ID] = struct{}{}
			device.ProviderID = id
			device.Capabilities = cloneCapabilities(device.Capabilities)
			devices = append(devices, device)
		}
	}
	return devices, nil
}

// Open checks the requested grant against the selected device before the
// provider sees it. The host must authorize the grant before calling Open.
func (r *Registry) Open(ctx context.Context, request OpenRequest) (*Connection, error) {
	if r == nil {
		return nil, errors.New("board registry is nil")
	}
	provider, exists := r.providers[request.ProviderID]
	if !exists {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, request.ProviderID)
	}
	devices, err := provider.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list board provider %q: %w", request.ProviderID, err)
	}
	var selected *Device
	for i := range devices {
		if devices[i].ID == request.DeviceID {
			selected = &devices[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownDevice, request.DeviceID)
	}
	if err := validateCapabilities(selected.Capabilities); err != nil {
		return nil, fmt.Errorf("board device %q: %w", request.DeviceID, err)
	}
	if len(request.Grant.Capabilities) == 0 {
		return nil, errors.New("board grant needs at least one capability")
	}
	advertised := make(map[Capability]struct{}, len(selected.Capabilities))
	for _, capability := range selected.Capabilities {
		advertised[capability.Name] = struct{}{}
	}
	allowed := make(map[Capability]struct{}, len(request.Grant.Capabilities))
	for _, capability := range request.Grant.Capabilities {
		if capability == "" {
			return nil, errors.New("board grant contains an empty capability")
		}
		if _, ok := advertised[capability]; !ok {
			return nil, fmt.Errorf("board device %q does not advertise %q", request.DeviceID, capability)
		}
		allowed[capability] = struct{}{}
	}
	if hasDriveCapability(allowed) && len(request.Grant.ResourceIDs) == 0 {
		return nil, errors.New("drive grant needs at least one resource ID")
	}
	for _, resourceID := range request.Grant.ResourceIDs {
		if strings.TrimSpace(resourceID) == "" {
			return nil, errors.New("board grant contains an empty resource ID")
		}
	}
	grant := cloneGrant(request.Grant)
	session, err := provider.Open(ctx, request.DeviceID, grant)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, errors.New("board provider returned a nil session")
	}
	resources := make(map[string]struct{}, len(grant.ResourceIDs))
	for _, resourceID := range grant.ResourceIDs {
		resources[resourceID] = struct{}{}
	}
	return &Connection{session: session, allowed: allowed, resources: resources}, nil
}

// Connection applies the host's capability grant to each invocation.
type Connection struct {
	mu        sync.Mutex
	session   Session
	allowed   map[Capability]struct{}
	resources map[string]struct{}
	closed    bool
}

func (c *Connection) Invoke(ctx context.Context, action Action) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.authorize(action); err != nil {
		return Result{}, err
	}
	return c.session.Invoke(ctx, action)
}

// authorize is the single gate every action passes through, whether it returns
// a Result or a content stream. Keeping one implementation means a new action
// path cannot forget a check the grant already depends on.
func (c *Connection) authorize(action Action) error {
	if c.closed {
		return ErrClosed
	}
	if _, allowed := c.allowed[action.Capability]; !allowed {
		return fmt.Errorf("%w: capability %q", ErrNotGranted, action.Capability)
	}
	if isDriveCapability(action.Capability) && strings.TrimSpace(action.ResourceID) == "" {
		return errors.New("drive action needs a resource ID")
	}
	if isDriveCapability(action.Capability) {
		if _, allowed := c.resources[action.ResourceID]; !allowed {
			return fmt.Errorf("%w: resource %q", ErrNotGranted, action.ResourceID)
		}
	}
	if len(action.Input) > 0 && !json.Valid(action.Input) {
		return errors.New("board action input is invalid JSON")
	}
	return nil
}

func (c *Connection) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.session.Close(ctx)
}

func isDriveCapability(capability Capability) bool {
	return capability == DriveList || capability == DriveRead
}

// IsDriveCapability reports whether any of the given capabilities acts on a
// drive resource root. A host that builds grants uses it to enforce the same
// rule Board does: a drive capability requires at least one resource ID.
func IsDriveCapability(capabilities ...Capability) bool {
	for _, capability := range capabilities {
		if isDriveCapability(capability) {
			return true
		}
	}
	return false
}

func hasDriveCapability(capabilities map[Capability]struct{}) bool {
	for capability := range capabilities {
		if isDriveCapability(capability) {
			return true
		}
	}
	return false
}

func cloneGrant(grant Grant) Grant {
	grant.Capabilities = append([]Capability(nil), grant.Capabilities...)
	grant.ResourceIDs = append([]string(nil), grant.ResourceIDs...)
	return grant
}

func validateCapabilities(capabilities []CapabilityDescriptor) error {
	seen := make(map[Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if strings.TrimSpace(string(capability.Name)) == "" || len(capability.InputSchema) == 0 || !json.Valid(capability.InputSchema) {
			return fmt.Errorf("invalid board capability descriptor %q", capability.Name)
		}
		if len(capability.OutputSchema) > 0 && !json.Valid(capability.OutputSchema) {
			return fmt.Errorf("invalid board output schema for %q", capability.Name)
		}
		if _, exists := seen[capability.Name]; exists {
			return fmt.Errorf("duplicate board capability %q", capability.Name)
		}
		seen[capability.Name] = struct{}{}
	}
	return nil
}

func cloneCapabilities(capabilities []CapabilityDescriptor) []CapabilityDescriptor {
	result := make([]CapabilityDescriptor, len(capabilities))
	for i, capability := range capabilities {
		result[i] = capability
		result[i].InputSchema = append(json.RawMessage(nil), capability.InputSchema...)
		result[i].OutputSchema = append(json.RawMessage(nil), capability.OutputSchema...)
	}
	return result
}
