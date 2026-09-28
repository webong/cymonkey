package board

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testRootID = "photos"

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustInput(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are unavailable here: %v", err)
	}
}

// driveFixture is one provider attached with the standard grant.
type driveFixture struct {
	registry   *Registry
	connection *Connection
	provider   *MountedDriveProvider
}

func newDriveFixture(t *testing.T, options MountedDriveOptions) driveFixture {
	t.Helper()
	provider, registry, connection := newDriveProvider(t, options)
	return driveFixture{registry: registry, connection: connection, provider: provider}
}

// newDriveProvider builds a provider over a host-approved root and attaches one
// session with the standard grant.
func newDriveProvider(t *testing.T, options MountedDriveOptions) (*MountedDriveProvider, *Registry, *Connection) {
	t.Helper()
	provider, err := NewMountedDriveProvider("test-mounted-drive", options)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := make([]Capability, 0, len(options.Capabilities))
	if len(options.Capabilities) == 0 {
		capabilities = []Capability{DriveList, DriveRead}
	} else {
		capabilities = append(capabilities, options.Capabilities...)
	}
	connection, err := registry.Open(context.Background(), OpenRequest{
		ProviderID: provider.ID(), DeviceID: provider.deviceID,
		Grant: Grant{Capabilities: capabilities, ResourceIDs: []string{testRootID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = connection.Close(context.Background())
		_ = provider.Close()
	})
	return provider, registry, connection
}

func TestMountedDriveListsAndReadsInsideTheApprovedRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "DCIM", "100CANON", "IMG_0001.JPG"), "jpeg-bytes")
	writeFile(t, filepath.Join(root, "notes.txt"), "hello")
	fixture := newDriveFixture(t, MountedDriveOptions{Root: root, RootID: testRootID, Name: "Field Camera USB"})

	devices, err := fixture.registry.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %#v, want one drive", devices)
	}
	device := devices[0]
	if device.ID != "drive" || device.Kind != "drive" || device.Name != "Field Camera USB" {
		t.Fatalf("device = %#v", device)
	}
	if len(device.Capabilities) != 2 {
		t.Fatalf("capabilities = %#v", device.Capabilities)
	}
	for _, capability := range device.Capabilities {
		if !json.Valid(capability.InputSchema) || !json.Valid(capability.OutputSchema) {
			t.Fatalf("capability %q published an invalid schema", capability.Name)
		}
	}

	listing := listEntries(t, fixture.connection, DriveList, map[string]any{})
	if len(listing.Entries) != 2 {
		t.Fatalf("entries = %#v", listing.Entries)
	}
	if listing.Path != "." || listing.Truncated || listing.Skipped != 0 {
		t.Fatalf("listing = %#v", listing)
	}
	if listing.Entries[0].Name != "DCIM" || listing.Entries[0].Kind != "directory" {
		t.Fatalf("first entry = %#v", listing.Entries[0])
	}

	nested := listEntries(t, fixture.connection, DriveList, map[string]any{"path": "DCIM/100CANON", "recursive": true})
	if len(nested.Entries) != 1 || nested.Entries[0].Path != "DCIM/100CANON/IMG_0001.JPG" {
		t.Fatalf("nested entries = %#v", nested.Entries)
	}
	if nested.Entries[0].Size != int64(len("jpeg-bytes")) {
		t.Fatalf("size = %d", nested.Entries[0].Size)
	}

	content, err := fixture.connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "DCIM/100CANON/IMG_0001.JPG"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	bytes, err := ReadAll(content, 1<<20)
	if err != nil || string(bytes) != "jpeg-bytes" {
		t.Fatalf("read = %q, %v", bytes, err)
	}
	if content.Truncated() {
		t.Fatal("a small file was reported as truncated")
	}
}

func TestMountedDriveRejectsInputsOutsideItsPublishedSchema(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photo.jpg"), "jpeg")
	fixture := newDriveFixture(t, MountedDriveOptions{Root: root, RootID: testRootID})
	ctx := context.Background()
	for _, input := range []string{`{"limit":-1}`, `{"extra":true}`, `{"limit":1} {}`} {
		if _, err := fixture.connection.Invoke(ctx, Action{Capability: DriveList, ResourceID: testRootID, Input: json.RawMessage(input)}); err == nil {
			t.Fatalf("drive.list accepted %q", input)
		}
	}
	for _, input := range []string{`{"path":"photo.jpg","maxBytes":-1}`, `{"path":"photo.jpg","extra":true}`} {
		if _, err := fixture.connection.Stream(ctx, Action{Capability: DriveRead, ResourceID: testRootID, Input: json.RawMessage(input)}); err == nil {
			t.Fatalf("drive.read accepted %q", input)
		}
	}
}

// TestMountedDriveRefusesToReturnContentsInAResult pins the shape decision that
// keeps Result.Output safe to log: file bytes go through Stream only.
func TestMountedDriveRefusesToReturnContentsInAResult(t *testing.T) {
	fixture := newDriveFixture(t, MountedDriveOptions{Root: t.TempDir(), RootID: testRootID})
	_, err := fixture.connection.Invoke(context.Background(), Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "anything.jpg"}),
	})
	if !errors.Is(err, ErrStreamRequired) {
		t.Fatalf("drive.read through Invoke = %v, want ErrStreamRequired", err)
	}
}

func TestMountedDriveRefusesPathsThatLeaveTheRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ok.txt"), "inside")
	fixture := newDriveFixture(t, MountedDriveOptions{Root: root, RootID: testRootID})

	for _, escape := range []string{
		"../secret.txt",
		"..",
		"../../etc/passwd",
		"nested/../../secret.txt",
		"/etc/passwd",
		"/",
		"ok\x00.txt",
	} {
		if _, err := fixture.connection.Stream(context.Background(), Action{
			Capability: DriveRead, ResourceID: testRootID,
			Input: mustInput(t, driveReadInput{Path: escape}),
		}); !errors.Is(err, errOutsideRoot) && !strings.Contains(errString(err), "leaves the granted root") &&
			!strings.Contains(errString(err), "must be relative") && !strings.Contains(errString(err), "NUL") {
			t.Fatalf("read %q = %v, want a refusal", escape, err)
		}
	}
}

func TestMountedDriveRefusesLinksThatLeaveTheRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "passwd"), "root:x:0:0")
	writeFile(t, filepath.Join(root, "ok.txt"), "inside")
	fixture := newDriveFixture(t, MountedDriveOptions{Root: root, RootID: testRootID})

	// A link to a file outside the mount.
	symlink(t, filepath.Join(outside, "passwd"), filepath.Join(root, "loose.txt"))
	if _, err := fixture.connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "loose.txt"}),
	}); !errors.Is(err, ErrDrivePath) {
		t.Fatalf("read through an outward file link = %v, want a refusal", err)
	}

	// A link to a directory outside the mount, reached through a read.
	symlink(t, outside, filepath.Join(root, "escape"))
	if _, err := fixture.connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "escape/passwd"}),
	}); !errors.Is(err, ErrDrivePath) {
		t.Fatalf("read through an outward directory link = %v, want a refusal", err)
	}

	// A recursive listing must not walk out through a link either, and must say
	// that something was withheld rather than reporting an empty drive.
	writeFile(t, filepath.Join(outside, "sneaky.jpg"), "not mine")
	writeFile(t, filepath.Join(root, "album", "inside.jpg"), "mine")
	listing := listEntries(t, fixture.connection, DriveList, map[string]any{"recursive": true})
	for _, entry := range listing.Entries {
		if strings.HasPrefix(entry.Path, "escape/") {
			t.Fatalf("recursive listing followed an outward link: %#v", listing.Entries)
		}
	}
	if listing.Skipped != 2 {
		t.Fatalf("skipped = %d, want 2 withheld outward links: %#v", listing.Skipped, listing.Entries)
	}
	// Recursion still works inside the root, and the outward links are absent
	// from the entries rather than being described as if they were readable.
	// Order is breadth-first, sorted by name within each level.
	paths := make([]string, 0, len(listing.Entries))
	for _, entry := range listing.Entries {
		paths = append(paths, entry.Path)
	}
	want := []string{"album", "ok.txt", "album/inside.jpg"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", paths, want)
	}
}

// TestMountedDriveRejectsASiblingThatSharesTheRootPrefix guards the containment
// check itself: "/photos" must not accept "/photos-backup".
func TestMountedDriveRejectsASiblingThatSharesTheRootPrefix(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "photos")
	outside := filepath.Join(base, "photos-backup")
	writeFile(t, filepath.Join(outside, "secret.jpg"), "not mine")
	writeFile(t, filepath.Join(root, "ok.jpg"), "mine")
	fixture := newDriveFixture(t, MountedDriveOptions{Root: root, RootID: testRootID})
	symlink(t, outside, filepath.Join(root, "neighbour"))

	if _, err := fixture.connection.Stream(context.Background(), Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "neighbour/secret.jpg"}),
	}); !errors.Is(err, ErrDrivePath) {
		t.Fatalf("read through a prefix-sharing sibling = %v, want a refusal", err)
	}
}

func TestMountedDriveFollowsLinksThatStayInsideTheRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real", "photo.jpg"), "mine")
	fixture := newDriveFixture(t, MountedDriveOptions{Root: root, RootID: testRootID})
	// os.Root allows links inside the root when their targets are relative.
	symlink(t, filepath.Join("real", "photo.jpg"), filepath.Join(root, "shortcut.jpg"))

	content, err := fixture.connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "shortcut.jpg"}),
	})
	if err != nil {
		t.Fatalf("a link inside the root was refused: %v", err)
	}
	defer content.Close()
	bytes, err := ReadAll(content, 1<<20)
	if err != nil || string(bytes) != "mine" {
		t.Fatalf("read = %q, %v", bytes, err)
	}
}

func TestMountedDriveBoundsEveryRead(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "big.bin"), strings.Repeat("A", 500))
	fixture := newDriveFixture(t, MountedDriveOptions{Root: root, RootID: testRootID, MaxReadBytes: 100})

	content, err := fixture.connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "big.bin"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	if content.Size() != 100 || !content.Truncated() {
		t.Fatalf("size = %d, truncated = %v", content.Size(), content.Truncated())
	}
	bytes, err := ReadAll(content, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes) != 100 {
		t.Fatalf("read %d bytes past a 100 byte bound", len(bytes))
	}
	// The bound is enforced by the stream, not by the caller's buffer size.
	if _, err := content.Read(make([]byte, 4096)); !errors.Is(err, io.EOF) {
		t.Fatalf("read past the bound = %v, want io.EOF", err)
	}

	// An action may tighten the provider's bound but never raise it.
	tightened, err := fixture.connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "big.bin", MaxBytes: 10}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tightened.Close()
	if tightened.Size() != 10 {
		t.Fatalf("tightened size = %d, want 10", tightened.Size())
	}
	widened, err := fixture.connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "big.bin", MaxBytes: 1 << 20}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer widened.Close()
	if widened.Size() != 100 {
		t.Fatalf("widened size = %d, want the provider bound of 100", widened.Size())
	}
}

func TestClosedDriveStreamsLeaveTheSession(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photo.jpg"), "jpeg")
	fixture := newDriveFixture(t, MountedDriveOptions{Root: root, RootID: testRootID})
	ctx := context.Background()
	session := fixture.connection.session.(*driveSession)
	for range 20 {
		content, err := fixture.connection.Stream(ctx, Action{
			Capability: DriveRead, ResourceID: testRootID,
			Input: mustInput(t, driveReadInput{Path: "photo.jpg"}),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := content.Close(); err != nil {
			t.Fatal(err)
		}
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if len(session.open) != 0 {
		t.Fatalf("session retained %d closed streams", len(session.open))
	}
}

func TestMountedDriveBoundsAndOrdersListings(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg", "d.jpg"} {
		writeFile(t, filepath.Join(root, name), name)
	}
	writeFile(t, filepath.Join(root, "album", "e.jpg"), "e")

	fixture := newDriveFixture(t, MountedDriveOptions{Root: root, RootID: testRootID, MaxListEntries: 2})
	listing := listEntries(t, fixture.connection, DriveList, map[string]any{"recursive": true})
	if len(listing.Entries) != 2 || !listing.Truncated {
		t.Fatalf("listing = %#v, want 2 entries flagged truncated", listing)
	}
	if listing.Entries[0].Path != "a.jpg" || listing.Entries[1].Path != "album" {
		t.Fatalf("listing is not in stable name order: %#v", listing.Entries)
	}
	// A smaller action limit is honored; a larger one is clamped to the grant.
	if got := listEntries(t, fixture.connection, DriveList, map[string]any{"limit": 1}); len(got.Entries) != 1 {
		t.Fatalf("action limit = %#v", got.Entries)
	}
}

func TestMountedDriveBoundsScanAndHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg", "d.jpg"} {
		writeFile(t, filepath.Join(root, name), name)
	}
	fixture := newDriveFixture(t, MountedDriveOptions{
		Root: root, RootID: testRootID, MaxListEntries: 2, MaxScanEntries: 2,
	})
	listing := listEntries(t, fixture.connection, DriveList, map[string]any{})
	if len(listing.Entries) > 2 || !listing.Truncated {
		t.Fatalf("scan-bound listing = %#v", listing)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fixture.connection.Invoke(ctx, Action{
		Capability: DriveList, ResourceID: testRootID, Input: json.RawMessage(`{}`),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled listing = %v", err)
	}
}

func TestMountedDriveAppliesTheHostGrant(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photo.jpg"), "mine")
	provider, err := NewMountedDriveProvider("test-mounted-drive", MountedDriveOptions{
		Root: root, RootID: testRootID, DeviceID: "usb-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}

	// A drive grant must name the provider's own resource root.
	_, err = registry.Open(ctx, OpenRequest{
		ProviderID: provider.ID(), DeviceID: "usb-1",
		Grant: Grant{Capabilities: []Capability{DriveList}, ResourceIDs: []string{"some-other-root"}},
	})
	if err == nil {
		t.Fatal("a grant naming an unknown resource root was accepted")
	}
	// A device the provider does not serve is refused.
	_, err = registry.Open(ctx, OpenRequest{
		ProviderID: provider.ID(), DeviceID: "usb-9",
		Grant: Grant{Capabilities: []Capability{DriveList}, ResourceIDs: []string{testRootID}},
	})
	if err == nil {
		t.Fatal("a grant for an unknown drive was accepted")
	}

	// A listing-only grant must not be able to read, and a capability the
	// provider was not built with must not be advertised at all.
	listingOnly, err := NewMountedDriveProvider("listing-only", MountedDriveOptions{
		Root: root, RootID: testRootID, Capabilities: []Capability{DriveList},
	})
	if err != nil {
		t.Fatal(err)
	}
	listingRegistry, err := NewRegistry(listingOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := listingRegistry.Open(ctx, OpenRequest{
		ProviderID: listingOnly.ID(), DeviceID: listingOnly.deviceID,
		Grant: Grant{Capabilities: []Capability{DriveRead}, ResourceIDs: []string{testRootID}},
	}); err == nil {
		t.Fatal("a provider without drive.read advertised it anyway")
	}
	connection, err := listingRegistry.Open(ctx, OpenRequest{
		ProviderID: listingOnly.ID(), DeviceID: listingOnly.deviceID,
		Grant: Grant{Capabilities: []Capability{DriveList}, ResourceIDs: []string{testRootID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)
	if _, err := connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "photo.jpg"}),
	}); err == nil {
		t.Fatal("a listing-only grant streamed a file")
	}
}

// TestClosingTheSessionClosesItsStreams matters for a host that hands a stream
// to a long-running import and then revokes the session.
func TestClosingTheSessionClosesItsStreams(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photo.jpg"), "mine")
	_, _, connection := newDriveProvider(t, MountedDriveOptions{Root: root, RootID: testRootID})

	content, err := connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "photo.jpg"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := content.Read(make([]byte, 8)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("read after the session closed = %v, want os.ErrClosed", err)
	}
	if _, err := connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "photo.jpg"}),
	}); !errors.Is(err, ErrClosed) {
		t.Fatalf("stream after close = %v, want ErrClosed", err)
	}
}

// TestMountedDriveNoticesAReplacedRoot models the USB case where a drive is
// unmounted and a different filesystem is mounted at the same path.
func TestMountedDriveNoticesAReplacedRoot(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	link := filepath.Join(base, "NOAA")
	original := filepath.Join(base, "noaa-volume")
	writeFile(t, filepath.Join(original, "photo.jpg"), "mine")
	symlink(t, original, link)

	_, registry, connection := newDriveProvider(t, MountedDriveOptions{
		Root: link, RootID: testRootID, Name: "NOAA",
	})
	if _, err := registry.List(ctx); err != nil {
		t.Fatal(err)
	}

	// Unmount the drive and mount something else where it used to be.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(base, "other-volume")
	writeFile(t, filepath.Join(replacement, "photo.jpg"), "not mine")
	if err := os.Symlink(replacement, link); err != nil {
		t.Fatal(err)
	}

	if _, err := registry.List(ctx); err == nil {
		t.Fatal("a replaced mount point was still advertised as the approved drive")
	}
	if _, err := connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "photo.jpg"}),
	}); err == nil {
		t.Fatal("an already open session read from a replaced mount point")
	}
}

func TestMountedDriveRejectsReplacementAtTheSamePath(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "mounted")
	writeFile(t, filepath.Join(root, "photo.jpg"), "original")
	provider, registry, connection := newDriveProvider(t, MountedDriveOptions{Root: root, RootID: testRootID})

	if err := os.Rename(root, filepath.Join(base, "old-mounted")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "photo.jpg"), "replacement")
	if _, err := registry.List(ctx); !errors.Is(err, ErrDriveUnavailable) {
		t.Fatalf("replacement was advertised: %v", err)
	}
	if _, err := connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "photo.jpg"}),
	}); !errors.Is(err, ErrDriveUnavailable) {
		t.Fatalf("replacement was read: %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMountedDriveRejectsUnusableRoots(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	writeFile(t, file, "x")
	for name, options := range map[string]MountedDriveOptions{
		"empty root":       {Root: ""},
		"missing root":     {Root: filepath.Join(t.TempDir(), "absent")},
		"file root":        {Root: file},
		"negative bound":   {Root: t.TempDir(), MaxReadBytes: -1},
		"negative limit":   {Root: t.TempDir(), MaxListEntries: -1},
		"negative scan":    {Root: t.TempDir(), MaxScanEntries: -1},
		"scan below list":  {Root: t.TempDir(), MaxListEntries: 3, MaxScanEntries: 2},
		"wrong capability": {Root: t.TempDir(), Capabilities: []Capability{KeyboardPress}},
	} {
		if _, err := NewMountedDriveProvider("test-mounted-drive", options); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if _, err := NewMountedDriveProvider("  ", MountedDriveOptions{Root: t.TempDir()}); err == nil {
		t.Fatal("a blank provider ID was accepted")
	}
}

func TestMountedDriveRejectsDirectoriesAndMalformedInput(t *testing.T) {
	fixture := newDriveFixture(t, MountedDriveOptions{Root: t.TempDir(), RootID: testRootID})
	writeFile(t, filepath.Join(fixture.provider.configuredRoot, "album", "a.jpg"), "a")
	ctx := context.Background()

	if _, err := fixture.connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID,
		Input: mustInput(t, driveReadInput{Path: "album"}),
	}); err == nil {
		t.Fatal("a directory was streamed as file content")
	}
	if _, err := fixture.connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID, Input: json.RawMessage(`{"path":12}`),
	}); err == nil {
		t.Fatal("a malformed read input was accepted")
	}
	if _, err := fixture.connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID, Input: json.RawMessage(`{}`),
	}); err == nil {
		t.Fatal("a read without a path was accepted")
	}
	if _, err := fixture.connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: testRootID, Input: json.RawMessage(`{"path":"a","maxBytes":"lots"}`),
	}); err == nil {
		t.Fatal("a non-numeric read bound was accepted")
	}
	if _, err := fixture.connection.Invoke(ctx, Action{
		Capability: DriveList, ResourceID: testRootID, Input: json.RawMessage(`[]`),
	}); err == nil {
		t.Fatal("a list input that is not an object was accepted")
	}
}

func listEntries(t *testing.T, connection *Connection, capability Capability, input any) driveListing {
	t.Helper()
	var payload json.RawMessage
	if input != nil {
		payload = mustInput(t, input)
	}
	result, err := connection.Invoke(context.Background(), Action{
		Capability: capability, ResourceID: testRootID, Input: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	var listing driveListing
	if err := json.Unmarshal(result.Output, &listing); err != nil {
		t.Fatalf("decode listing %s: %v", result.Output, err)
	}
	return listing
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
