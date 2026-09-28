package boardcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"board"
	boardhost "board/host"
)

// Board drives are mounted by the host, not found by Board. Naming a root on
// the command line is the host's approval: the provider is registered for that
// directory alone, and every action is confined to it.

// driveProviderPrefix names the Board provider registered for a CLI root. Board
// keys registrations by provider ID, so one prefix per root keeps them distinct.
const driveProviderPrefix = "mounted-drive-"

// rootNamePattern constrains the host-supplied handle a root is published under.
// The handle reaches a caller as an action's resourceId, so it is held to the
// same simple identifier shape as Board capability names.
var rootNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`)

// driveRoot is one host-approved mounted directory named as NAME=PATH.
type driveRoot struct {
	ResourceID string
	Path       string
}

type driveRootFlags []driveRoot

func (f *driveRootFlags) String() string {
	names := make([]string, 0, len(*f))
	for _, root := range *f {
		names = append(names, root.ResourceID+"="+root.Path)
	}
	return strings.Join(names, ",")
}

func (f *driveRootFlags) Set(value string) error {
	name, path, found := strings.Cut(strings.TrimSpace(value), "=")
	name, path = strings.TrimSpace(name), strings.TrimSpace(path)
	if !found || name == "" || path == "" {
		return errors.New("root must be NAME=PATH")
	}
	if !rootNamePattern.MatchString(name) {
		return errors.New("root name must use a protocol-style handle")
	}
	for _, existing := range *f {
		if existing.ResourceID == name {
			return fmt.Errorf("root %q is named twice", name)
		}
	}
	*f = append(*f, driveRoot{ResourceID: name, Path: path})
	return nil
}

// driveRoots converts command-line roots into the host approvals Board is
// registered with. Only board.DriveList and board.DriveRead are offered, and
// both by default, because a mounted drive is a read-only device here.
func driveRoots(roots driveRootFlags) []boardhost.DriveRoot {
	approvals := make([]boardhost.DriveRoot, 0, len(roots))
	for _, root := range roots {
		approvals = append(approvals, boardhost.DriveRoot{
			ID:           driveProviderPrefix + root.ResourceID,
			Name:         root.ResourceID,
			Path:         root.Path,
			ResourceID:   root.ResourceID,
			Capabilities: []board.Capability{board.DriveList, board.DriveRead},
		})
	}
	return approvals
}

// approveDrive is the host's decision for a command-line invocation. The
// operator named the root, so listing inside it is approved and nothing else is.
func approveDrive(resourceID string, capability board.Capability) boardhost.Authorizer {
	return func(_ context.Context, _ board.Device, _ boardhost.Request) (boardhost.Decision, error) {
		return boardhost.Decision{
			Capabilities: []board.Capability{capability},
			ResourceIDs:  []string{resourceID},
		}, nil
	}
}

// devicesCommand reports the mounted drives the host approved, with the
// capabilities and resource handles a caller may use.
func devicesCommand(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("devices", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var roots driveRootFlags
	flags.Var(&roots, "root", "host-approved mounted root as NAME=PATH (repeatable)")
	jsonOutput := flags.Bool("json", false, "write the device inventory as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("devices accepts flags only")
	}
	if len(roots) == 0 {
		return errors.New("at least one --root is required")
	}
	devices, err := boardhost.New(driveRoots(roots)...)
	if err != nil {
		return err
	}
	defer devices.Close()
	descriptors, err := devices.List(context.Background())
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(stdout).Encode(map[string]any{
			"devices": descriptors,
		})
	}
	for _, device := range descriptors {
		names := make([]string, 0, len(device.Capabilities))
		for _, capability := range device.Capabilities {
			names = append(names, string(capability.Name))
		}
		if _, err := fmt.Fprintf(stdout, "%s/%s\t%s\t%s\t%s\n",
			device.ProviderID, device.ID, device.Kind,
			strings.Join(names, ","), strings.Join(device.ResourceIDs, ","),
		); err != nil {
			return err
		}
	}
	return nil
}

// driveListRequest mirrors the published board.DriveList input schema. The host
// binds to the schema Board advertises rather than to its private types, which
// is what lets Board keep changing its internals.
type driveListRequest struct {
	Path      string `json:"path,omitempty"`
	Recursive bool   `json:"recursive,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// driveListing mirrors the published board.DriveList output schema.
type driveListing struct {
	Path      string       `json:"path"`
	Entries   []driveEntry `json:"entries"`
	Truncated bool         `json:"truncated"`
	Skipped   int          `json:"skipped"`
}

type driveEntry struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// driveListCommand attaches one host-approved root and lists what is inside it.
// It is the shortest real path through the whole chain: host approval, provider
// registration, grant selection, the drive provider's containment rules, and a
// bounded JSON result.
func driveListCommand(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("drive-list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var roots driveRootFlags
	flags.Var(&roots, "root", "host-approved mounted root as NAME=PATH")
	path := flags.String("path", "", "directory relative to the granted root")
	recursive := flags.Bool("recursive", false, "include the children of subdirectories")
	limit := flags.Int("limit", 0, "maximum entries to return, clamped to the provider bound")
	jsonOutput := flags.Bool("json", false, "write the listing as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("drive-list accepts flags only")
	}
	if len(roots) != 1 {
		return errors.New("drive-list requires exactly one --root")
	}
	ctx := context.Background()
	devices, err := boardhost.New(driveRoots(roots)...)
	if err != nil {
		return err
	}
	defer devices.Close()
	connection, err := devices.Attach(ctx, boardhost.Request{
		Capabilities: []board.Capability{board.DriveList},
		ResourceIDs:  []string{roots[0].ResourceID},
	}, approveDrive(roots[0].ResourceID, board.DriveList))
	if err != nil {
		return err
	}
	defer connection.Close(ctx)

	input, err := json.Marshal(driveListRequest{Path: *path, Recursive: *recursive, Limit: *limit})
	if err != nil {
		return err
	}
	result, err := connection.Invoke(ctx, board.Action{
		Capability: board.DriveList,
		ResourceID: roots[0].ResourceID,
		Input:      input,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		_, err := stdout.Write(append(result.Output, '\n'))
		return err
	}
	var listing driveListing
	if err := json.Unmarshal(result.Output, &listing); err != nil {
		return fmt.Errorf("decode board drive listing: %w", err)
	}
	for _, entry := range listing.Entries {
		if _, err := fmt.Fprintf(stdout, "%s\t%s\t%d\t%s\n",
			entry.Kind, entry.Path, entry.Size, entry.ModTime.Format(time.RFC3339),
		); err != nil {
			return err
		}
	}
	if listing.Truncated || listing.Skipped > 0 {
		_, err := fmt.Fprintf(stdout, "truncated=%v skipped=%d\n", listing.Truncated, listing.Skipped)
		return err
	}
	return nil
}

// driveReadCommand streams one selected file to stdout. The operator chooses
// the destination with shell redirection or a pipe; Board never writes to a
// filesystem destination on the caller's behalf.
func driveReadCommand(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("drive-read", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var roots driveRootFlags
	flags.Var(&roots, "root", "host-approved mounted root as NAME=PATH")
	path := flags.String("path", "", "file relative to the granted root")
	maxBytes := flags.Int64("max-bytes", 0, "tighten the provider's per-file read bound")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("drive-read accepts flags only")
	}
	if len(roots) != 1 {
		return errors.New("drive-read requires exactly one --root")
	}
	if strings.TrimSpace(*path) == "" {
		return errors.New("drive-read requires --path")
	}
	if *maxBytes < 0 {
		return errors.New("drive-read max-bytes cannot be negative")
	}
	ctx := context.Background()
	devices, err := boardhost.New(driveRoots(roots)...)
	if err != nil {
		return err
	}
	defer devices.Close()
	connection, err := devices.Attach(ctx, boardhost.Request{
		Capabilities: []board.Capability{board.DriveRead},
		ResourceIDs:  []string{roots[0].ResourceID},
	}, approveDrive(roots[0].ResourceID, board.DriveRead))
	if err != nil {
		return err
	}
	defer connection.Close(ctx)
	input, err := json.Marshal(struct {
		Path     string `json:"path"`
		MaxBytes int64  `json:"maxBytes,omitempty"`
	}{Path: *path, MaxBytes: *maxBytes})
	if err != nil {
		return err
	}
	content, err := connection.Stream(ctx, board.Action{
		Capability: board.DriveRead,
		ResourceID: roots[0].ResourceID,
		Input:      input,
	})
	if err != nil {
		return err
	}
	defer content.Close()
	if content.Truncated() {
		return fmt.Errorf("drive-read refuses partial content: file exceeds the %d byte bound", content.Size())
	}
	written, err := io.Copy(stdout, content)
	if err != nil {
		return err
	}
	if written != content.Size() {
		return io.ErrUnexpectedEOF
	}
	return nil
}
