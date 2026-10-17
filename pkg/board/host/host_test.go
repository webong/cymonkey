package host

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"board"
)

// testProviderPrefix mirrors the CLI's provider naming so the tests exercise
// registration order the way a real host configures it.
const testProviderPrefix = "mounted-drive-"

func newRoot(t *testing.T, id, path string) DriveRoot {
	t.Helper()
	return DriveRoot{ID: testProviderPrefix + id, Name: id, Path: path, ResourceID: id}
}

func populatedRoot(t *testing.T, id string) DriveRoot {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "IMG_0001.JPG"), []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "DCIM", "100CANON"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "DCIM", "100CANON", "IMG_0002.JPG"), []byte("jpeg2"), 0o644); err != nil {
		t.Fatal(err)
	}
	return newRoot(t, id, dir)
}

// refuseAlways stands in for a host that approves nothing. It is used where the
// test needs a non-nil authorizer but expects the request to fail first.
func refuseAlways(context.Context, board.Device, Request) (Decision, error) {
	return Decision{}, errors.New("this authorizer approves nothing")
}

type fakeKeyboardProvider struct{ invoked int }

func (*fakeKeyboardProvider) ID() string { return "selected-keyboard" }
func (*fakeKeyboardProvider) List(context.Context) ([]board.Device, error) {
	return []board.Device{{ID: "keyboard", Kind: "keyboard", Capabilities: []board.CapabilityDescriptor{{
		Name: board.KeyboardPress, InputSchema: json.RawMessage(`{"type":"object"}`),
	}}}}, nil
}
func (p *fakeKeyboardProvider) Open(context.Context, string, board.Grant) (board.Session, error) {
	return &fakeKeyboardSession{provider: p}, nil
}

type fakeKeyboardSession struct{ provider *fakeKeyboardProvider }

func (s *fakeKeyboardSession) Invoke(_ context.Context, _ board.Action) (board.Result, error) {
	s.provider.invoked++
	return board.Result{Output: json.RawMessage(`{"submitted":true}`)}, nil
}
func (*fakeKeyboardSession) Close(context.Context) error { return nil }

func TestRegisteredKeyboardUsesHostAuthorization(t *testing.T) {
	ctx := context.Background()
	provider := &fakeKeyboardProvider{}
	devices, err := NewRegistered(Registration{Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := devices.Attach(ctx, Request{Capabilities: []board.Capability{board.KeyboardPress}},
		func(_ context.Context, device board.Device, _ Request) (Decision, error) {
			if device.ProviderID != provider.ID() || device.Kind != "keyboard" {
				return Decision{}, errors.New("wrong keyboard target")
			}
			return Decision{Capabilities: []board.Capability{board.KeyboardPress}}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)
	if _, err := connection.Invoke(ctx, board.Action{
		Capability: board.KeyboardPress, Input: json.RawMessage(`{"key":"Enter"}`),
	}); err != nil || provider.invoked != 1 {
		t.Fatalf("keyboard action = %d, %v", provider.invoked, err)
	}
}

func TestDevicesListOnlyHostApprovedRoots(t *testing.T) {
	root := populatedRoot(t, "photos")
	devices, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	descriptors, err := devices.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptors) != 1 {
		t.Fatalf("devices = %#v, want the one approved root", descriptors)
	}
	device := descriptors[0]
	if device.ProviderID != testProviderPrefix+"photos" || device.ID != "drive" || device.Kind != "drive" {
		t.Fatalf("device = %#v", device)
	}
	if len(device.ResourceIDs) != 1 || device.ResourceIDs[0] != "photos" {
		t.Fatalf("resource IDs = %#v", device.ResourceIDs)
	}
	// The host filesystem path is the provider's business. Publishing it beside
	// the device would turn an approved root into a discoverable one.
	encoded, err := json.Marshal(descriptors)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), root.Path) {
		t.Fatalf("device inventory published the host path: %s", encoded)
	}
}

func TestNewRejectsUnusableRoots(t *testing.T) {
	if _, err := New(DriveRoot{ID: "a", Path: filepath.Join(t.TempDir(), "absent")}); err == nil {
		t.Fatal("a missing root was registered")
	}
	if _, err := New(DriveRoot{ID: "", Path: t.TempDir()}); err == nil {
		t.Fatal("a root without a provider ID was registered")
	}
	// Two roots whose provider IDs collapse to the same device must be caught
	// rather than silently shadowing each other.
	first := newRoot(t, "same", t.TempDir())
	if _, err := New(first, newRoot(t, "same", t.TempDir())); err == nil {
		t.Fatal("two roots with the same provider and device ID were registered")
	}
	empty, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if descriptors, err := empty.List(context.Background()); err != nil || len(descriptors) != 0 {
		t.Fatalf("an empty surface reported devices: %#v, %v", descriptors, err)
	}
}

func TestAttachEnforcesTheHostGrant(t *testing.T) {
	ctx := context.Background()
	devices, err := New(populatedRoot(t, "photos"))
	if err != nil {
		t.Fatal(err)
	}
	var authorized board.Device
	connection, err := devices.Attach(ctx,
		Request{Capabilities: []board.Capability{board.DriveList, board.DriveRead}},
		func(_ context.Context, device board.Device, _ Request) (Decision, error) {
			authorized = device
			return Decision{
				Capabilities: []board.Capability{board.DriveList, board.DriveRead},
				ResourceIDs:  []string{"photos"},
			}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)
	if authorized.ID != "drive" {
		t.Fatalf("the authorizer saw %#v", authorized)
	}

	// Listing works, and the result stays small structured JSON.
	result, err := connection.Invoke(ctx, board.Action{
		Capability: board.DriveList, ResourceID: "photos", Input: json.RawMessage(`{"recursive":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Entries []struct {
			Path string `json:"path"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(result.Output, &listing); err != nil {
		t.Fatal(err)
	}
	// DCIM, IMG_0001.JPG, DCIM/100CANON, DCIM/100CANON/IMG_0002.JPG
	if len(listing.Entries) != 4 {
		t.Fatalf("entries = %#v", listing.Entries)
	}

	// Reading is granted, but only as a stream; the Result shape never carries
	// file bytes.
	if _, err := connection.Invoke(ctx, board.Action{
		Capability: board.DriveRead, ResourceID: "photos", Input: json.RawMessage(`{"path":"IMG_0001.JPG"}`),
	}); !errors.Is(err, board.ErrStreamRequired) {
		t.Fatalf("drive.read through Invoke = %v, want ErrStreamRequired", err)
	}
	content, err := connection.Stream(ctx, board.Action{
		Capability: board.DriveRead, ResourceID: "photos", Input: json.RawMessage(`{"path":"IMG_0001.JPG"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	bytes, err := board.ReadAll(content, 1<<20)
	if err != nil || string(bytes) != "jpeg" {
		t.Fatalf("read = %q, %v", bytes, err)
	}
}

func TestAttachRefusesAGrantTheDeviceCannotServe(t *testing.T) {
	devices, err := New(populatedRoot(t, "photos"))
	if err != nil {
		t.Fatal(err)
	}
	// A host decision that names a root the device never published.
	_, err = devices.Attach(context.Background(),
		Request{Capabilities: []board.Capability{board.DriveList}},
		func(context.Context, board.Device, Request) (Decision, error) {
			return Decision{Capabilities: []board.Capability{board.DriveList}, ResourceIDs: []string{"not-approved"}}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "not-approved") {
		t.Fatalf("an unpublished resource was granted: %v", err)
	}
	// A drive capability with no resource root names nothing to confine to.
	_, err = devices.Attach(context.Background(),
		Request{Capabilities: []board.Capability{board.DriveList}},
		func(context.Context, board.Device, Request) (Decision, error) {
			return Decision{Capabilities: []board.Capability{board.DriveList}}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "resource root") {
		t.Fatalf("a drive grant without a resource root was accepted: %v", err)
	}
}

// TestAttachRefusesAGrantBeyondTheRequest is the escalation guard: a decision
// may narrow what a caller asked for, never widen it.
func TestAttachRefusesAGrantBeyondTheRequest(t *testing.T) {
	devices, err := New(populatedRoot(t, "photos"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = devices.Attach(context.Background(),
		Request{Capabilities: []board.Capability{board.DriveList}},
		func(context.Context, board.Device, Request) (Decision, error) {
			return Decision{
				Capabilities: []board.Capability{board.DriveList, board.DriveRead},
				ResourceIDs:  []string{"photos"},
			}, nil
		})
	if err == nil || !strings.Contains(err.Error(), "unrequested capability") {
		t.Fatalf("a grant beyond the request was accepted: %v", err)
	}
}

func TestAttachRefusesUnadvertisedCapabilities(t *testing.T) {
	dir := t.TempDir()
	devices, err := New(DriveRoot{
		ID: "listing-only", Name: "listing-only", Path: dir, ResourceID: "photos",
		Capabilities: []board.Capability{board.DriveList},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The device never advertised drive.read, so no decision can grant it.
	_, err = devices.Attach(context.Background(),
		Request{Capabilities: []board.Capability{board.DriveRead}},
		func(context.Context, board.Device, Request) (Decision, error) {
			return Decision{Capabilities: []board.Capability{board.DriveRead}, ResourceIDs: []string{"photos"}}, nil
		})
	if err == nil {
		t.Fatal("an unadvertised capability was granted")
	}
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("error = %v, want ErrNotAuthorized", err)
	}
}

func TestAttachSkipsADeviceTheHostRefuses(t *testing.T) {
	ctx := context.Background()
	// Two approved roots. Provider IDs sort, so "a" is always considered first.
	devices, err := New(populatedRoot(t, "a"), populatedRoot(t, "b"))
	if err != nil {
		t.Fatal(err)
	}
	var considered []string
	connection, err := devices.Attach(ctx, Request{Capabilities: []board.Capability{board.DriveList}},
		func(_ context.Context, device board.Device, _ Request) (Decision, error) {
			considered = append(considered, device.ID)
			if device.ProviderID == testProviderPrefix+"a" {
				return Decision{}, errors.New("that drive is not approved for this session")
			}
			return Decision{Capabilities: []board.Capability{board.DriveList}, ResourceIDs: []string{"b"}}, nil
		})
	if err != nil {
		t.Fatalf("a refusal for one device ended the whole request: %v", err)
	}
	defer connection.Close(ctx)
	if len(considered) != 2 {
		t.Fatalf("the authorizer considered %#v, want both devices", considered)
	}
	// The second root is a different directory, so its listing is its own.
	result, err := connection.Invoke(ctx, board.Action{
		Capability: board.DriveList, ResourceID: "b", Input: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.Output), "IMG_0001.JPG") {
		t.Fatalf("listing = %s", result.Output)
	}
	if _, err := connection.Invoke(ctx, board.Action{
		Capability: board.DriveList, ResourceID: "a", Input: json.RawMessage(`{}`),
	}); err == nil {
		t.Fatal("the refused device's root was granted through the surviving connection")
	}
}

func TestAttachReportsEveryRefusal(t *testing.T) {
	devices, err := New(populatedRoot(t, "a"), populatedRoot(t, "b"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = devices.Attach(context.Background(), Request{Capabilities: []board.Capability{board.DriveList}},
		func(context.Context, board.Device, Request) (Decision, error) {
			return Decision{}, errors.New("no device is approved right now")
		})
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("error = %v, want ErrNotAuthorized", err)
	}
	if !strings.Contains(err.Error(), "no device is approved right now") {
		t.Fatalf("error = %v, want the host's reason", err)
	}
}

func TestAttachRequiresAHostAuthorizer(t *testing.T) {
	ctx := context.Background()
	devices, err := New(populatedRoot(t, "photos"))
	if err != nil {
		t.Fatal(err)
	}
	// Board deliberately leaves authorization to the host, so a binding with no
	// answer to give must refuse rather than assume one.
	if _, err := devices.Attach(ctx, Request{Capabilities: []board.Capability{board.DriveList}}, nil); err == nil {
		t.Fatal("a request with no authorizer was attached")
	}
	for name, request := range map[string]Request{
		"no capabilities":  {},
		"blank capability": {Capabilities: []board.Capability{""}},
	} {
		if _, err := devices.Attach(ctx, request, refuseAlways); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	var unconfigured *Devices
	if _, err := unconfigured.Attach(ctx, Request{Capabilities: []board.Capability{board.DriveList}}, refuseAlways); err == nil {
		t.Fatal("an unconfigured surface attached a device")
	}
}

func TestRequestedCapabilitiesAreDeduplicatedAndOrdered(t *testing.T) {
	t.Parallel()

	unique, err := requestedCapabilities([]board.Capability{board.DriveRead, board.DriveList, board.DriveRead})
	if err != nil {
		t.Fatal(err)
	}
	if len(unique) != 2 || unique[0] != board.DriveList || unique[1] != board.DriveRead {
		t.Fatalf("capabilities = %#v, want a sorted deduplicated list", unique)
	}
	if !board.IsDriveCapability(unique...) {
		t.Fatal("the drive capabilities were not recognized as drive capabilities")
	}
}

func TestAttachAcceptsARepeatedGrant(t *testing.T) {
	ctx := context.Background()
	devices, err := New(populatedRoot(t, "photos"))
	if err != nil {
		t.Fatal(err)
	}
	// A decision that names a capability or resource more than once is a host
	// mistake, not an escalation, and must not produce a different grant.
	connection, err := devices.Attach(ctx,
		Request{Capabilities: []board.Capability{board.DriveList, board.DriveList}},
		func(context.Context, board.Device, Request) (Decision, error) {
			return Decision{
				Capabilities: []board.Capability{board.DriveList, board.DriveList},
				ResourceIDs:  []string{"photos", "photos"},
			}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)
	if _, err := connection.Invoke(ctx, board.Action{
		Capability: board.DriveList, ResourceID: "photos", Input: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
}
