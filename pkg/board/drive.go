package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Mounted-drive defaults. They are deliberately small: a photo import reads
// many modest files, and a bound the host has not tuned should fail closed.
const (
	// DefaultDriveRootID is the opaque resource ID a mounted-drive provider
	// uses when the host does not name its own approved root.
	DefaultDriveRootID = "root"
	// DefaultMaxDriveReadBytes bounds one drive.read stream.
	DefaultMaxDriveReadBytes int64 = 32 << 20
	// DefaultMaxDriveListEntries bounds one drive.list result.
	DefaultMaxDriveListEntries = 2000
	// DefaultMaxDriveScanEntries bounds entries inspected by one listing.
	DefaultMaxDriveScanEntries = 10000
)

// errOutsideRoot is returned for a textual path that leaves the approved root.
var errOutsideRoot = errors.New("board drive path leaves the granted root")

// ErrDrivePath reports an inaccessible path inside a drive action. os.Root
// rejects outward links before any content is opened; callers should treat
// this error as a refused path without assuming why it was inaccessible.
var ErrDrivePath = errors.New("board drive path is unavailable or outside its root")

// ErrDriveUnavailable means the originally approved directory is gone or has
// been replaced. Callers must obtain a new host approval before reopening it.
var ErrDriveUnavailable = errors.New("board approved drive is unavailable")

// MountedDriveOptions configures one read-only mounted-drive provider. The host
// supplies Root; the provider never discovers, mounts, or chooses a volume, and
// it never writes, renames, or removes anything.
type MountedDriveOptions struct {
	// Root is the single host-approved directory this provider may expose.
	Root string
	// DeviceID is the stable handle a grant names this drive by. It defaults
	// to "drive".
	DeviceID string
	// RootID is the opaque resource ID that drive actions must name. It
	// defaults to DefaultDriveRootID. Board never interprets it, so a host can
	// use a label such as "photos" without publishing a host path.
	RootID string
	// Name is an optional human label for the device.
	Name string
	// Capabilities selects a subset of DriveList and DriveRead. It defaults to
	// both; omit DriveRead for a listing-only mount.
	Capabilities []Capability
	// MaxReadBytes bounds one drive.read stream. It defaults to
	// DefaultMaxDriveReadBytes and cannot be raised by an action.
	MaxReadBytes int64
	// MaxListEntries bounds one drive.list result. It defaults to
	// DefaultMaxDriveListEntries and cannot be raised by an action.
	MaxListEntries int
	// MaxScanEntries bounds work spent inspecting a directory tree. It
	// defaults to DefaultMaxDriveScanEntries and must cover MaxListEntries.
	MaxScanEntries int
}

// MountedDriveProvider exposes one already-mounted, host-approved directory as a
// read-only drive device. It is the provider behind the USB photo import use
// case: the host approves a mount point, and every listing and read is confined
// to it.
//
// The provider pins the approved directory identity. Each attachment uses an
// os.Root handle, and each action checks that the host path still names the
// original directory before accessing anything inside that handle.
type MountedDriveProvider struct {
	providerID     string
	configuredRoot string
	approvedInfo   os.FileInfo
	approvedHandle *os.Root
	mu             sync.Mutex
	closed         bool
	deviceID       string
	rootID         string
	name           string
	capabilities   []CapabilityDescriptor
	capabilitySet  map[Capability]struct{}
	maxReadBytes   int64
	maxListEntries int
	maxScanEntries int
}

var _ Provider = (*MountedDriveProvider)(nil)

// NewMountedDriveProvider resolves the host-approved root once and records
// where it pointed. Rejecting a bad root here means a misconfigured mount fails
// at registration rather than at the first agent action.
func NewMountedDriveProvider(providerID string, options MountedDriveOptions) (*MountedDriveProvider, error) {
	// os.Root uses name-based checks on these platforms and cannot provide the
	// containment guarantee this provider advertises.
	if runtime.GOOS == "js" || runtime.GOOS == "plan9" {
		return nil, fmt.Errorf("mounted drive provider is unavailable on %s", runtime.GOOS)
	}
	if strings.TrimSpace(providerID) == "" {
		return nil, errors.New("board drive provider needs a nonempty ID")
	}
	configured := strings.TrimSpace(options.Root)
	if configured == "" {
		return nil, errors.New("board drive provider needs a host-approved root")
	}
	if options.MaxReadBytes < 0 {
		return nil, errors.New("board drive max read bytes cannot be negative")
	}
	if options.MaxListEntries < 0 {
		return nil, errors.New("board drive max list entries cannot be negative")
	}
	if options.MaxScanEntries < 0 {
		return nil, errors.New("board drive max scan entries cannot be negative")
	}
	absolute, err := filepath.Abs(configured)
	if err != nil {
		return nil, fmt.Errorf("board drive root %q is not a usable path: %w", configured, err)
	}
	approvedHandle, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, fmt.Errorf("board drive root %q is unavailable: %w", configured, err)
	}
	info, err := approvedHandle.Stat(".")
	if err != nil {
		_ = approvedHandle.Close()
		return nil, fmt.Errorf("board drive root %q is unavailable: %w", configured, err)
	}
	if !info.IsDir() {
		_ = approvedHandle.Close()
		return nil, fmt.Errorf("board drive root %q is not a directory", configured)
	}

	rootID := strings.TrimSpace(options.RootID)
	if rootID == "" {
		rootID = DefaultDriveRootID
	}
	maxReadBytes := options.MaxReadBytes
	if maxReadBytes == 0 {
		maxReadBytes = DefaultMaxDriveReadBytes
	}
	maxListEntries := options.MaxListEntries
	if maxListEntries == 0 {
		maxListEntries = DefaultMaxDriveListEntries
	}
	maxScanEntries := options.MaxScanEntries
	if maxScanEntries == 0 {
		maxScanEntries = DefaultMaxDriveScanEntries
	}
	if maxScanEntries < maxListEntries {
		_ = approvedHandle.Close()
		return nil, errors.New("board drive scan bound must cover its list bound")
	}

	requested := options.Capabilities
	if len(requested) == 0 {
		requested = []Capability{DriveList, DriveRead}
	}
	provider := &MountedDriveProvider{
		providerID:     strings.TrimSpace(providerID),
		configuredRoot: absolute,
		approvedInfo:   info,
		approvedHandle: approvedHandle,
		deviceID:       driveDeviceID(options.DeviceID),
		rootID:         rootID,
		name:           strings.TrimSpace(options.Name),
		maxReadBytes:   maxReadBytes,
		maxListEntries: maxListEntries,
		maxScanEntries: maxScanEntries,
		capabilitySet:  make(map[Capability]struct{}, len(requested)),
	}
	for _, capability := range requested {
		descriptor, err := driveCapabilityDescriptor(capability)
		if err != nil {
			_ = approvedHandle.Close()
			return nil, err
		}
		provider.capabilitySet[capability] = struct{}{}
		provider.capabilities = append(provider.capabilities, descriptor)
	}
	return provider, nil
}

func driveDeviceID(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return "drive"
}

// ID returns the name this provider was registered under.
func (p *MountedDriveProvider) ID() string { return p.providerID }

// DeviceID returns the handle a grant must name to select this drive.
func (p *MountedDriveProvider) DeviceID() string { return p.deviceID }

// RootID returns the opaque resource handle every drive action must name. It is
// the provider's own label for the approved root; the host filesystem path
// behind it is never part of the action contract.
func (p *MountedDriveProvider) RootID() string { return p.rootID }

// Close releases the provider's pinned root. Existing sessions have their own
// handles but reject new actions once the provider has closed.
func (p *MountedDriveProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	return p.approvedHandle.Close()
}

func (p *MountedDriveProvider) List(ctx context.Context) ([]Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A vanished mount is reported as an unavailable device rather than as an
	// absent one, so a caller asking "is this drive still there" does not have
	// to tell those two cases apart.
	if err := p.rootStillApproved(); err != nil {
		return nil, err
	}
	return []Device{{
		ID:           p.deviceID,
		Kind:         "drive",
		Name:         p.name,
		Capabilities: cloneCapabilities(p.capabilities),
	}}, nil
}

// Open applies the host grant itself. Board has already checked that the
// requested capabilities were advertised, but only this provider knows what its
// resource IDs mean, so that mapping is resolved here.
func (p *MountedDriveProvider) Open(ctx context.Context, deviceID string, grant Grant) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deviceID != p.deviceID {
		return nil, fmt.Errorf("board drive device %q is not registered", deviceID)
	}
	// Confirm the mount is still the one the host approved before handing out
	// an attachment, so a session never exists for a root that has moved.
	root, err := p.openApprovedRoot()
	if err != nil {
		return nil, err
	}
	defer func() {
		if root != nil {
			_ = root.Close()
		}
	}()
	capabilities := make(map[Capability]struct{}, len(grant.Capabilities))
	for _, capability := range grant.Capabilities {
		if _, ok := p.capabilitySet[capability]; !ok {
			return nil, fmt.Errorf("board drive %q does not provide %q", p.deviceID, capability)
		}
		capabilities[capability] = struct{}{}
	}
	if len(capabilities) == 0 {
		return nil, errors.New("board drive grant needs at least one capability")
	}
	resources := make(map[string]struct{}, len(grant.ResourceIDs))
	for _, resourceID := range grant.ResourceIDs {
		if resourceID != p.rootID {
			return nil, fmt.Errorf("board drive %q does not provide resource %q", p.deviceID, resourceID)
		}
		resources[resourceID] = struct{}{}
	}
	if hasDriveCapability(capabilities) && len(resources) == 0 {
		return nil, fmt.Errorf("board drive %q requires resource %q in the grant", p.deviceID, p.rootID)
	}
	session := &driveSession{
		provider:     p,
		root:         root,
		capabilities: capabilities,
		resources:    resources,
		open:         make(map[*driveContent]struct{}),
	}
	root = nil
	return session, nil
}

// openApprovedRoot opens the host path and compares directory identity, not
// just its text. A handle to the original directory remains pinned, preventing
// identity reuse while the provider is alive.
func (p *MountedDriveProvider) openApprovedRoot() (*os.Root, error) {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return nil, ErrClosed
	}
	root, err := os.OpenRoot(p.configuredRoot)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDriveUnavailable, err)
	}
	info, err := root.Stat(".")
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("%w: %v", ErrDriveUnavailable, err)
	}
	if !info.IsDir() || !os.SameFile(info, p.approvedInfo) {
		_ = root.Close()
		return nil, ErrDriveUnavailable
	}
	p.mu.Lock()
	closed = p.closed
	p.mu.Unlock()
	if closed {
		_ = root.Close()
		return nil, ErrClosed
	}
	return root, nil
}

func (p *MountedDriveProvider) rootStillApproved() error {
	root, err := p.openApprovedRoot()
	if err != nil {
		return err
	}
	return root.Close()
}

// relativeDrivePath normalizes one action path. Action input is JSON, so paths
// are always slash-separated regardless of host platform, and a path is always
// relative to the granted root.
func relativeDrivePath(value string) (string, error) {
	if strings.ContainsRune(value, 0) {
		return "", errors.New("board drive path contains a NUL byte")
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "." {
		return ".", nil
	}
	hosted := filepath.FromSlash(trimmed)
	if filepath.IsAbs(hosted) || filepath.VolumeName(hosted) != "" {
		return "", errors.New("board drive path must be relative to the granted root")
	}
	cleaned := filepath.Clean(hosted)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errOutsideRoot
	}
	return cleaned, nil
}

// driveEntry is one listed child of a granted directory. Path is relative to
// the root and slash-separated; the host filesystem path is deliberately not
// echoed back into agent-visible output.
type driveEntry struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// driveListing is the drive.list result.
type driveListing struct {
	Path string `json:"path"`
	// Entries are sorted by name within each directory level.
	Entries []driveEntry `json:"entries"`
	// Truncated reports that the entry or scan limit ended the listing early.
	Truncated bool `json:"truncated"`
	// Skipped counts children the provider refused to describe, because they
	// resolve outside the granted root or could not be inspected. Withholding
	// them is the point; reporting the count keeps that from looking like an
	// empty drive.
	Skipped int `json:"skipped"`
}

// driveListInput is the drive.list action input.
type driveListInput struct {
	// Path is a directory relative to the granted root. Omit it for the root.
	Path string `json:"path,omitempty"`
	// Recursive includes the children of subdirectories.
	Recursive bool `json:"recursive,omitempty"`
	// Limit caps the returned entries and is clamped to the provider bound.
	Limit int `json:"limit,omitempty"`
}

// driveReadInput is the drive.read action input.
type driveReadInput struct {
	// Path is a file relative to the granted root.
	Path string `json:"path"`
	// MaxBytes narrows the provider's read bound for this one action. It can
	// only tighten the grant, never widen it.
	MaxBytes int64 `json:"maxBytes,omitempty"`
}

func driveCapabilityDescriptor(capability Capability) (CapabilityDescriptor, error) {
	switch capability {
	case DriveList:
		return CapabilityDescriptor{
			Name:         DriveList,
			InputSchema:  json.RawMessage(driveListInputSchema),
			OutputSchema: json.RawMessage(driveListingOutputSchema),
		}, nil
	case DriveRead:
		return CapabilityDescriptor{
			Name:         DriveRead,
			InputSchema:  json.RawMessage(driveReadInputSchema),
			OutputSchema: json.RawMessage(driveReadOutputSchema),
		}, nil
	default:
		return CapabilityDescriptor{}, fmt.Errorf("mounted drive provider does not provide %q", capability)
	}
}

// driveSession is one host-approved attachment to the approved root.
type driveSession struct {
	provider     *MountedDriveProvider
	root         *os.Root
	capabilities map[Capability]struct{}
	resources    map[string]struct{}

	mu     sync.Mutex
	closed bool
	open   map[*driveContent]struct{}
}

var (
	_ Session          = (*driveSession)(nil)
	_ StreamingSession = (*driveSession)(nil)
)

// Invoke answers the bounded JSON capabilities. File bytes never appear here.
func (s *driveSession) Invoke(ctx context.Context, action Action) (Result, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return Result{}, ErrClosed
	}
	if err := s.authorize(action); err != nil {
		return Result{}, err
	}
	switch action.Capability {
	case DriveRead:
		return Result{}, fmt.Errorf("%w: %q", ErrStreamRequired, action.Capability)
	case DriveList:
		listing, err := s.list(ctx, action.Input)
		if err != nil {
			return Result{}, err
		}
		encoded, err := json.Marshal(listing)
		if err != nil {
			return Result{}, fmt.Errorf("encode board drive listing: %w", err)
		}
		return Result{Output: encoded}, nil
	default:
		return Result{}, fmt.Errorf("board drive session cannot invoke %q", action.Capability)
	}
}

// Stream opens one bounded read. The bound lives in the returned Content, so a
// caller cannot read past it by ignoring Size.
func (s *driveSession) Stream(ctx context.Context, action Action) (Content, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, ErrClosed
	}
	if err := s.authorize(action); err != nil {
		return nil, err
	}
	if action.Capability != DriveRead {
		return nil, fmt.Errorf("board drive session cannot stream %q", action.Capability)
	}
	var input driveReadInput
	if len(action.Input) > 0 {
		if err := decodeDriveInput(action.Input, &input); err != nil {
			return nil, fmt.Errorf("decode board drive read input: %w", err)
		}
	}
	if input.MaxBytes < 0 {
		return nil, errors.New("board drive read maxBytes cannot be negative")
	}
	if strings.TrimSpace(input.Path) == "" {
		return nil, errors.New("board drive read needs a path")
	}
	limit := s.provider.maxReadBytes
	if input.MaxBytes > 0 && input.MaxBytes < limit {
		limit = input.MaxBytes
	}
	if limit <= 0 {
		return nil, errors.New("board drive read bound is zero")
	}

	// The session's root handle remains on the approved directory even if a
	// path changes. Refuse a new action when the host path no longer names it.
	if err := s.provider.rootStillApproved(); err != nil {
		return nil, err
	}
	path, err := relativeDrivePath(input.Path)
	if err != nil {
		return nil, err
	}
	file, err := s.root.Open(path)
	if err != nil {
		return nil, drivePathError(path, err)
	}
	content, err := openDriveContent(ctx, file, limit)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = content.Close()
		return nil, ErrClosed
	}
	content.onClose = func(closed *driveContent) {
		s.mu.Lock()
		delete(s.open, closed)
		s.mu.Unlock()
	}
	s.open[content] = struct{}{}
	return content, nil
}

// Close releases the attachment. Streams handed out earlier are closed too, so a
// host that loses track of one cannot keep reading after it ends the session.
func (s *driveSession) Close(context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	open := make([]*driveContent, 0, len(s.open))
	for content := range s.open {
		open = append(open, content)
	}
	s.open = make(map[*driveContent]struct{})
	s.mu.Unlock()
	for _, content := range open {
		_ = content.Close()
	}
	return s.root.Close()
}

// authorize repeats the grant check inside the provider. Board gates an action
// before it arrives, but the provider is the only component that knows which
// resource IDs it actually serves, and a direct caller of Provider.Open must
// not be able to skip the check.
func (s *driveSession) authorize(action Action) error {
	if _, allowed := s.capabilities[action.Capability]; !allowed {
		return fmt.Errorf("board drive capability %q is not granted", action.Capability)
	}
	if _, allowed := s.resources[action.ResourceID]; !allowed {
		return fmt.Errorf("board drive resource %q is not granted", action.ResourceID)
	}
	return nil
}

func (s *driveSession) list(ctx context.Context, input json.RawMessage) (driveListing, error) {
	if err := ctx.Err(); err != nil {
		return driveListing{}, err
	}
	var request driveListInput
	if len(input) > 0 {
		if err := decodeDriveInput(input, &request); err != nil {
			return driveListing{}, fmt.Errorf("decode board drive list input: %w", err)
		}
	}
	if request.Limit < 0 {
		return driveListing{}, errors.New("board drive list limit cannot be negative")
	}
	limit := s.provider.maxListEntries
	if request.Limit > 0 && request.Limit < limit {
		limit = request.Limit
	}
	if err := s.provider.rootStillApproved(); err != nil {
		return driveListing{}, err
	}
	directory, err := relativeDrivePath(request.Path)
	if err != nil {
		return driveListing{}, err
	}
	listing := driveListing{Path: filepath.ToSlash(directory), Entries: []driveEntry{}}

	// Breadth-first over the children inspected within the scan bound, sorting
	// their names before adding them to the result.
	truncated := false
	queue := []string{directory}
	scanned := 0
	for len(queue) > 0 && !truncated {
		if err := ctx.Err(); err != nil {
			return driveListing{}, err
		}
		current := queue[0]
		queue = queue[1:]
		opened, err := s.root.Open(current)
		if err != nil {
			if current == directory {
				return driveListing{}, drivePathError(current, err)
			}
			listing.Skipped++
			continue
		}
		info, err := opened.Stat()
		if err != nil {
			_ = opened.Close()
			return driveListing{}, err
		}
		if !info.IsDir() {
			_ = opened.Close()
			return driveListing{}, fmt.Errorf("board drive path %q is not a directory", current)
		}
		children, more, err := readDriveDirectory(ctx, opened, &scanned, s.provider.maxScanEntries)
		_ = opened.Close()
		if err != nil {
			if current == directory {
				return driveListing{}, err
			}
			listing.Skipped++
			continue
		}
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			if err := ctx.Err(); err != nil {
				return driveListing{}, err
			}
			if len(listing.Entries) >= limit {
				// More children exist than this action may return, so the
				// listing is explicitly incomplete rather than silently short.
				truncated = true
				break
			}
			childPath := filepath.Join(current, child.Name())
			entry, ok := s.describe(childPath, child.Name())
			if !ok {
				listing.Skipped++
				continue
			}
			listing.Entries = append(listing.Entries, entry)
			if request.Recursive && entry.Kind == "directory" {
				queue = append(queue, childPath)
			}
		}
		if more || scanned >= s.provider.maxScanEntries && len(queue) > 0 {
			truncated = true
		}
	}
	listing.Truncated = truncated
	return listing, nil
}

// readDriveDirectory caps metadata retained for one listing, even when the
// underlying directory contains millions of entries. A scan-bound result is
// marked truncated; the names inspected so far are sorted by the caller.
func readDriveDirectory(ctx context.Context, directory *os.File, scanned *int, limit int) ([]os.DirEntry, bool, error) {
	var entries []os.DirEntry
	for *scanned < limit {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		batchSize := limit - *scanned
		if batchSize > 128 {
			batchSize = 128
		}
		batch, err := directory.ReadDir(batchSize)
		entries = append(entries, batch...)
		*scanned += len(batch)
		if errors.Is(err, io.EOF) {
			return entries, false, nil
		}
		if err != nil {
			return nil, false, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	more, err := directory.ReadDir(1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	return entries, len(more) > 0, nil
}

// describe turns one child path into a listed entry. It reports false when the
// child resolves outside the granted root or cannot be inspected, which is how a
// link pointing out of the mount is withheld instead of advertised. Names are
// reported as they appear in the directory, not as their link targets resolve.
func (s *driveSession) describe(childPath, name string) (driveEntry, bool) {
	info, err := s.root.Stat(childPath)
	if err != nil {
		return driveEntry{}, false
	}
	// Path and Name are reported as the directory shows them, not as the entry's
	// link target resolves, so a caller browsing the drive sees the names it
	// would see with any other tool.
	entry := driveEntry{
		Path:    filepath.ToSlash(childPath),
		Name:    name,
		Kind:    "file",
		Size:    info.Size(),
		ModTime: info.ModTime().UTC(),
	}
	switch {
	case info.IsDir():
		entry.Kind = "directory"
		entry.Size = 0
	case !info.Mode().IsRegular():
		// Sockets, devices, and FIFOs are neither readable content nor useful
		// entries for a photo import.
		return driveEntry{}, false
	}
	return entry, true
}

// driveContent is a bounded read of one approved file.
type driveContent struct {
	ctx       context.Context
	file      *os.File
	remaining int64
	size      int64
	truncated bool

	mu      sync.Mutex
	closed  bool
	onClose func(*driveContent)
}

var _ Content = (*driveContent)(nil)

// openDriveContent takes a file already opened through os.Root and fixes how
// many bytes the stream can yield. It owns file from this point onward.
func openDriveContent(ctx context.Context, file *os.File, limit int64) (*driveContent, error) {
	fail := func(err error) (*driveContent, error) {
		_ = file.Close()
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		return fail(fmt.Errorf("inspect board drive entry: %w", err))
	}
	if !opened.Mode().IsRegular() {
		return fail(errors.New("board drive entry is not a regular file"))
	}
	size := opened.Size()
	if size > limit {
		size = limit
	}
	return &driveContent{
		ctx:       ctx,
		file:      file,
		remaining: size,
		size:      size,
		truncated: opened.Size() > limit,
	}, nil
}

// os.Root refuses links that escape its directory. Preserve a stable Board
// error for inaccessible paths while leaving ordinary missing-file errors
// distinguishable with errors.Is(err, os.ErrNotExist).
func drivePathError(path string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("board drive path %q: %w", path, err)
	}
	return fmt.Errorf("%w: %v", ErrDrivePath, err)
}

func decodeDriveInput(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("drive input has extra JSON values")
		}
		return err
	}
	return nil
}

func (c *driveContent) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, os.ErrClosed
	}
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	if c.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	read, err := c.file.Read(p)
	c.remaining -= int64(read)
	if errors.Is(err, io.EOF) && c.remaining > 0 {
		return read, io.ErrUnexpectedEOF
	}
	return read, err
}

// Size is the total number of bytes this stream can return.
func (c *driveContent) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.size
}

// Truncated reports that the file on the device is larger than this stream.
func (c *driveContent) Truncated() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.truncated
}

func (c *driveContent) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	err := c.file.Close()
	onClose := c.onClose
	c.mu.Unlock()
	if onClose != nil {
		onClose(c)
	}
	return err
}
