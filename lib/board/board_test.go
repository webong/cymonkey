package board

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeProvider struct {
	devices []Device
	grant   Grant
	session *fakeSession
}

func capability(name Capability) CapabilityDescriptor {
	return CapabilityDescriptor{Name: name, InputSchema: json.RawMessage(`{"type":"object"}`)}
}

func (p *fakeProvider) ID() string { return "test" }

func (p *fakeProvider) List(context.Context) ([]Device, error) {
	return append([]Device(nil), p.devices...), nil
}

func (p *fakeProvider) Open(_ context.Context, _ string, grant Grant) (Session, error) {
	p.grant = grant
	p.session = &fakeSession{}
	return p.session, nil
}

type fakeSession struct {
	invocations []Action
	closed      bool
}

func (s *fakeSession) Invoke(_ context.Context, action Action) (Result, error) {
	s.invocations = append(s.invocations, action)
	return Result{Output: json.RawMessage(`{"ok":true}`)}, nil
}

func (s *fakeSession) Close(context.Context) error {
	s.closed = true
	return nil
}

func TestRegistryListsAndGatesKeyboard(t *testing.T) {
	ctx := context.Background()
	provider := &fakeProvider{devices: []Device{{
		ID: "keyboard-1", Kind: "keyboard", Capabilities: []CapabilityDescriptor{capability(KeyboardPress), capability(KeyboardType)},
	}}}
	registry, err := NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := registry.List(ctx)
	if err != nil || len(devices) != 1 || devices[0].ProviderID != "test" {
		t.Fatalf("unexpected device discovery: %#v, %v", devices, err)
	}
	connection, err := registry.Open(ctx, OpenRequest{
		ProviderID: "test", DeviceID: "keyboard-1",
		Grant: Grant{Capabilities: []Capability{KeyboardPress}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Invoke(ctx, Action{Capability: KeyboardType, Input: json.RawMessage(`{"text":"secret"}`)}); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("ungranted keyboard typing = %v, want ErrNotGranted", err)
	}
	if len(provider.session.invocations) != 0 {
		t.Fatal("ungranted action reached provider")
	}
	if _, err := connection.Invoke(ctx, Action{Capability: KeyboardPress, Input: json.RawMessage(`{"key":"Enter"}`)}); err != nil {
		t.Fatal(err)
	}
	if len(provider.session.invocations) != 1 {
		t.Fatalf("got %d provider invocations, want 1", len(provider.session.invocations))
	}
	if err := connection.Close(ctx); err != nil || !provider.session.closed {
		t.Fatalf("connection did not close provider session: %v", err)
	}
	if _, err := connection.Invoke(ctx, Action{Capability: KeyboardPress}); !errors.Is(err, ErrClosed) {
		t.Fatalf("invoke after close = %v, want ErrClosed", err)
	}
}

func TestDriveGrantRequiresResourceScope(t *testing.T) {
	ctx := context.Background()
	provider := &fakeProvider{devices: []Device{{
		ID: "usb-1", Kind: "drive", Capabilities: []CapabilityDescriptor{capability(DriveList), capability(DriveRead)},
	}}}
	registry, err := NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	request := OpenRequest{ProviderID: "test", DeviceID: "usb-1", Grant: Grant{Capabilities: []Capability{DriveList}}}
	if _, err := registry.Open(ctx, request); err == nil || provider.session != nil {
		t.Fatalf("unscoped drive grant was accepted: %v", err)
	}
	request.Grant.ResourceIDs = []string{"photos-root"}
	connection, err := registry.Open(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.grant.ResourceIDs) != 1 || provider.grant.ResourceIDs[0] != "photos-root" {
		t.Fatalf("provider received wrong resource scope: %#v", provider.grant)
	}
	if _, err := connection.Invoke(ctx, Action{Capability: DriveList}); err == nil {
		t.Fatal("drive action without resource ID was accepted")
	}
	if _, err := connection.Invoke(ctx, Action{Capability: DriveList, ResourceID: "other-root"}); !errors.Is(err, ErrNotGranted) {
		t.Fatalf("drive action outside granted resource root = %v, want ErrNotGranted", err)
	}
	if _, err := connection.Invoke(ctx, Action{Capability: DriveList, ResourceID: "photos-root"}); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Invoke(ctx, Action{Capability: DriveRead, ResourceID: "photos-root"}); err == nil {
		t.Fatal("ungranted drive read was accepted")
	}
}

func TestRegistryRejectsUnknownCapabilitiesAndDuplicateProviders(t *testing.T) {
	provider := &fakeProvider{devices: []Device{{ID: "keyboard-1", Kind: "keyboard", Capabilities: []CapabilityDescriptor{capability(KeyboardPress)}}}}
	if _, err := NewRegistry(provider, provider); err == nil {
		t.Fatal("duplicate provider was accepted")
	}
	registry, err := NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Open(context.Background(), OpenRequest{
		ProviderID: "test", DeviceID: "keyboard-1",
		Grant: Grant{Capabilities: []Capability{DriveRead}, ResourceIDs: []string{"photos-root"}},
	})
	if err == nil || provider.session != nil {
		t.Fatalf("unadvertised capability was accepted: %v", err)
	}
}

func TestRegistryIdentifiesUnknownProviderAndDevice(t *testing.T) {
	provider := &fakeProvider{devices: []Device{{ID: "keyboard-1", Kind: "keyboard", Capabilities: []CapabilityDescriptor{capability(KeyboardPress)}}}}
	registry, err := NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Open(context.Background(), OpenRequest{ProviderID: "absent"}); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("unknown provider = %v", err)
	}
	if _, err := registry.Open(context.Background(), OpenRequest{ProviderID: "test", DeviceID: "absent"}); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("unknown device = %v", err)
	}
}
