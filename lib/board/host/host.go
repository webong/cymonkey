// Package host lets an application register Board providers and authorize
// device grants. The application chooses providers and decides which
// capabilities and resources a caller may use; Board enforces the result.
package host

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"board"
)

// ErrNotAuthorized reports that no host-approved device served a request. A
// caller that wants to distinguish "no such device" from "the host said no"
// should compare against this.
var ErrNotAuthorized = errors.New("no host-approved board device serves this request")

// DriveRoot is one mounted directory the host has approved for device access.
// The host supplies Path; Board resolves it and confines every action to it.
// Nothing here discovers, mounts, or unmounts a volume.
type DriveRoot struct {
	// ID names the Board provider. It must be unique across the roots.
	ID string
	// Name is an optional human label for the device.
	Name string
	// Path is the host filesystem directory the host approved.
	Path string
	// DeviceID is the handle a grant names this drive by.
	DeviceID string
	// ResourceID is the opaque handle an action must name to use this root. It
	// is deliberately not Path: a caller addresses the root by handle, and the
	// host path never becomes part of an action contract.
	ResourceID string
	// Capabilities selects a subset of board.DriveList and board.DriveRead.
	Capabilities []board.Capability
	// MaxReadBytes, MaxListEntries, and MaxScanEntries bound one action.
	MaxReadBytes   int64
	MaxListEntries int
	MaxScanEntries int
}

// Devices is the registered device surface: a Board registry plus the resource
// handles the host published for it. Board keeps its own descriptors
// provider-neutral, so the host view is what carries those handles.
type Devices struct {
	registry  *board.Registry
	resources map[string][]string
	closers   []interface{ Close() error }
}

// Registration makes a caller-owned provider available to the host. The host
// publishes only the named resource handles; keyboard providers use none.
type Registration struct {
	Provider    board.Provider
	ResourceIDs []string
}

// Descriptor is a host view of one device, including the opaque resource
// handles a caller may name.
type Descriptor struct {
	board.Device
	ResourceIDs []string `json:"resourceIds"`
}

// New registers one Board provider per approved root. Passing no roots yields an
// empty surface rather than a provider that could find something on its own:
// device access is granted by the host, not discovered by this package.
func New(roots ...DriveRoot) (*Devices, error) {
	registrations := make([]Registration, 0, len(roots))
	created := make([]*board.MountedDriveProvider, 0, len(roots))
	complete := false
	defer func() {
		if !complete {
			for _, provider := range created {
				_ = provider.Close()
			}
		}
	}()
	for _, root := range roots {
		provider, err := board.NewMountedDriveProvider(root.ID, board.MountedDriveOptions{
			Root:           root.Path,
			Name:           root.Name,
			DeviceID:       root.DeviceID,
			RootID:         root.ResourceID,
			Capabilities:   root.Capabilities,
			MaxReadBytes:   root.MaxReadBytes,
			MaxListEntries: root.MaxListEntries,
			MaxScanEntries: root.MaxScanEntries,
		})
		if err != nil {
			return nil, fmt.Errorf("register board drive %q: %w", root.ID, err)
		}
		created = append(created, provider)
		registrations = append(registrations, Registration{
			Provider: provider, ResourceIDs: []string{provider.RootID()},
		})
	}
	devices, err := NewRegistered(registrations...)
	if err != nil {
		return nil, err
	}
	complete = true
	return devices, nil
}

// NewRegistered accepts explicit Board providers from a host. Board still
// supplies all device operations; the host supplies grant selection.
func NewRegistered(registrations ...Registration) (*Devices, error) {
	providers := make([]board.Provider, 0, len(registrations))
	devices := &Devices{resources: make(map[string][]string)}
	for _, registration := range registrations {
		if registration.Provider == nil {
			return nil, errors.New("board registration needs a provider")
		}
		listed, err := registration.Provider.List(context.Background())
		if err != nil {
			return nil, fmt.Errorf("list board provider %q: %w", registration.Provider.ID(), err)
		}
		for _, device := range listed {
			key := deviceKey(registration.Provider.ID(), device.ID)
			if _, exists := devices.resources[key]; exists {
				return nil, fmt.Errorf("board device %s is registered twice", key)
			}
			devices.resources[key] = append([]string(nil), registration.ResourceIDs...)
		}
		providers = append(providers, registration.Provider)
		if closer, ok := registration.Provider.(interface{ Close() error }); ok {
			devices.closers = append(devices.closers, closer)
		}
	}
	registry, err := board.NewRegistry(providers...)
	if err != nil {
		return nil, fmt.Errorf("register board providers: %w", err)
	}
	devices.registry = registry
	return devices, nil
}

// Close releases the registered providers' pinned directory handles. Callers
// should close their connections before closing the device surface.
func (d *Devices) Close() error {
	if d == nil {
		return nil
	}
	var result error
	for _, closer := range d.closers {
		result = errors.Join(result, closer.Close())
	}
	return result
}

// List returns the registered devices in Board discovery order, each with the
// resource handles the host published for it. Only approved roots appear, so a
// caller may present this result to an agent without filtering it first.
func (d *Devices) List(ctx context.Context) ([]Descriptor, error) {
	if d == nil || d.registry == nil {
		return nil, errors.New("board device surface is not configured")
	}
	discovered, err := d.registry.List(ctx)
	if err != nil {
		return nil, err
	}
	descriptors := make([]Descriptor, 0, len(discovered))
	for _, device := range discovered {
		descriptors = append(descriptors, Descriptor{
			Device:      device,
			ResourceIDs: append([]string(nil), d.resources[deviceKey(device.ProviderID, device.ID)]...),
		})
	}
	return descriptors, nil
}

// Request is what a caller asked for. It names capabilities and opaque resource
// handles only; a host path never belongs in a request.
type Request struct {
	Capabilities []board.Capability
	ResourceIDs  []string
}

// Decision is the host's answer for one candidate device. An empty Decision
// denies the request, and Reason is then reported back to the caller.
type Decision struct {
	Capabilities []board.Capability
	ResourceIDs  []string
	Reason       string
}

// Authorizer decides whether one host-approved device may serve a request. It is
// required and is never defaulted. Board leaves authorization to the host, and a
// binding that invented a default answer would be the thing it exists to prevent.
type Authorizer func(context.Context, board.Device, Request) (Decision, error)

// Attach selects a device, applies the host's decision, and returns the
// connection that enforces it.
//
// Selection is deterministic: among the devices advertising every requested
// capability, the first in Board discovery order wins, so the same request
// attaches the same device twice. A host that refuses one device is treated as a
// refusal of that device, not of the request, so the search continues and the
// reasons are reported together if nothing is approved.
func (d *Devices) Attach(ctx context.Context, request Request, authorize Authorizer) (*board.Connection, error) {
	if d == nil || d.registry == nil {
		return nil, errors.New("board device surface is not configured")
	}
	if authorize == nil {
		return nil, errors.New("board attachment requires a host authorizer")
	}
	capabilities, err := requestedCapabilities(request.Capabilities)
	if err != nil {
		return nil, err
	}
	discovered, err := d.registry.List(ctx)
	if err != nil {
		return nil, err
	}
	var refusals []string
	for _, device := range discovered {
		if !advertisesAll(device, capabilities) {
			continue
		}
		key := deviceKey(device.ProviderID, device.ID)
		decision, err := authorize(ctx, device, request)
		if err != nil {
			refusals = append(refusals, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		grant, err := d.authorizedGrant(device, request, decision)
		if err != nil {
			refusals = append(refusals, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		return d.registry.Open(ctx, board.OpenRequest{
			ProviderID: device.ProviderID, DeviceID: device.ID, Grant: grant,
		})
	}
	if len(refusals) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotAuthorized, strings.Join(refusals, "; "))
	}
	return nil, fmt.Errorf("%w: no registered device advertises [%s]",
		ErrNotAuthorized, strings.Join(capabilityNames(capabilities), ", "))
}

// authorizedGrant turns a host decision into the grant Board will enforce, and
// refuses any decision that would exceed what the caller asked for. That check
// is the reason the binding exists: a decision may narrow a request, never
// widen it, so a misconfigured or buggy authorizer cannot quietly hand out a
// capability nobody requested.
func (d *Devices) authorizedGrant(device board.Device, request Request, decision Decision) (board.Grant, error) {
	if len(decision.Capabilities) == 0 {
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "the host authorized no capability"
		}
		return board.Grant{}, errors.New(reason)
	}
	granted, err := requestedCapabilities(decision.Capabilities)
	if err != nil {
		return board.Grant{}, err
	}
	asked, err := requestedCapabilities(request.Capabilities)
	if err != nil {
		return board.Grant{}, err
	}
	permitted := make(map[board.Capability]struct{}, len(asked))
	for _, capability := range asked {
		permitted[capability] = struct{}{}
	}
	for _, capability := range granted {
		if _, ok := permitted[capability]; !ok {
			return board.Grant{}, fmt.Errorf("the host granted unrequested capability %q", capability)
		}
		if !advertises(device, capability) {
			return board.Grant{}, fmt.Errorf("the host granted capability %q that %s does not advertise", capability, deviceKey(device.ProviderID, device.ID))
		}
	}

	published := d.resources[deviceKey(device.ProviderID, device.ID)]
	resources, err := d.grantedResources(published, decision.ResourceIDs)
	if err != nil {
		return board.Grant{}, err
	}
	if board.IsDriveCapability(granted...) && len(resources) == 0 {
		return board.Grant{}, errors.New("the host granted a drive capability without naming a resource root")
	}
	return board.Grant{Capabilities: granted, ResourceIDs: resources}, nil
}

// grantedResources keeps a decision to handles the host actually published. The
// binding is the only component that knows those handles, which is what stops a
// decision from naming a root that was never approved for this device.
func (d *Devices) grantedResources(published, requested []string) ([]string, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	known := make(map[string]struct{}, len(published))
	for _, resourceID := range published {
		known[resourceID] = struct{}{}
	}
	granted := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, resourceID := range requested {
		trimmed := strings.TrimSpace(resourceID)
		if trimmed == "" {
			return nil, errors.New("the host granted an empty resource ID")
		}
		if _, ok := known[trimmed]; !ok {
			return nil, fmt.Errorf("the host granted resource %q, which this device does not publish", trimmed)
		}
		if _, duplicate := seen[trimmed]; duplicate {
			continue
		}
		seen[trimmed] = struct{}{}
		granted = append(granted, trimmed)
	}
	return granted, nil
}

func requestedCapabilities(values []board.Capability) ([]board.Capability, error) {
	if len(values) == 0 {
		return nil, errors.New("a board request needs at least one capability")
	}
	seen := make(map[board.Capability]struct{}, len(values))
	unique := make([]board.Capability, 0, len(values))
	for _, capability := range values {
		if strings.TrimSpace(string(capability)) == "" {
			return nil, errors.New("a board request contains an empty capability")
		}
		if _, duplicate := seen[capability]; duplicate {
			continue
		}
		seen[capability] = struct{}{}
		unique = append(unique, capability)
	}
	// Stable order keeps the grant, and therefore any audit of it, reproducible.
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	return unique, nil
}

func capabilityNames(capabilities []board.Capability) []string {
	names := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		names = append(names, string(capability))
	}
	return names
}

func advertisesAll(device board.Device, capabilities []board.Capability) bool {
	for _, capability := range capabilities {
		if !advertises(device, capability) {
			return false
		}
	}
	return true
}

func advertises(device board.Device, capability board.Capability) bool {
	for _, descriptor := range device.Capabilities {
		if descriptor.Name == capability {
			return true
		}
	}
	return false
}

func deviceKey(providerID, deviceID string) string {
	return providerID + "/" + deviceID
}
