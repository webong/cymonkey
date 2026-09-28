# Board

Board is a standalone input and output device library used by Cymonkey. It
provides a small contract for discovering devices, opening a host-approved
session, and invoking bounded capabilities. A Board provider binds that
contract to a particular operating system, device class, or transport.

Board ships a read-only mounted-drive provider and a macOS keyboard provider.
The host registers each provider and authorizes each attachment. Board does not
discover, mount, or manage physical devices. There is no hotplug event source.

## First workflow: import photos from a USB drive

1. A registered drive provider reports the USB drive and its capabilities.
2. The host approves `drive.list` and `drive.read` for a selected resource
   root. The drive provider resolves that opaque root and confines all listing
   and reading to it.
3. The macOS keyboard provider can separately receive a grant for
   `keyboard.press` or `keyboard.type` for an explicitly selected process.
4. The host coordinates file selection and app interaction through
   `board/host` or its own Board registry.

The first capability names are `keyboard.press`, `keyboard.type`, `drive.list`,
and `drive.read`. Providers may define more names for other devices. A grant
names one provider, one device, and a subset of that device's advertised
capabilities. Drive grants also require opaque root IDs. Each drive action
must name one granted root; the provider must confine any path inside the
action input beneath that root, including aliases and links. Board cannot
resolve provider-specific resources itself.

## The mounted-drive provider

`NewMountedDriveProvider` takes a host-approved root. It never discovers,
mounts, or unmounts a volume, and it never writes. `RootID` is the opaque
handle an action must name; the host filesystem path is deliberately not part
of the action contract and is not echoed back into results.

An action path must be relative, contain no NUL byte, and stay within the root
after `..` normalization. Each session opens the root with `os.Root`; listing
and reading use that handle, including when a path component changes during an
action. Relative symlinks that resolve inside the root work. Links that leave
the root are refused; a listing withholds inaccessible entries and increments
`skipped`. The provider also pins the original directory and compares its
identity with the host path on every new action. A removed or replaced mount
returns `ErrDriveUnavailable` and needs a new host approval.

`MaxListEntries` bounds the result, `MaxScanEntries` bounds directory entries
inspected, and `MaxReadBytes` bounds each content stream. A partial listing is
marked `truncated`; `drive-read` in Board's CLI refuses partial files.
`os.Root` confinement does not prevent traversal into a mount nested inside
the approved directory. A host that needs a single-volume policy must approve
a root without nested mounts or enforce that policy separately.

## Results and streaming

`Result.Output` is small structured JSON, which keeps it safe to log, compare,
and forward. File bytes do not belong in it. A provider that implements
`StreamingSession` delivers contents through `Connection.Stream`, which returns
a `Content`: a forward-only reader with a fixed `Size`, a `Truncated` flag, and
a `Close`. The bound lives in the stream, so a caller cannot read past it by
ignoring the size, and an action's `maxBytes` can only tighten the provider's
bound. `Connection.Stream` runs the same grant gate as `Connection.Invoke`, so
an ungranted capability or resource fails before any provider code runs, and
closing a session closes the streams it handed out.

`drive.read` through `Invoke` returns `ErrStreamRequired` rather than
inlining content into a result. `board.ReadAll` exists for a host that genuinely
needs bytes in memory and requires it to state its own ceiling.

## Extend Board

Implement `Provider` and `Session`, then register the provider explicitly with
`NewRegistry`. Each device advertises capability names and JSON input schemas.
The provider lists only devices it can access, validates each action's input,
applies the supplied grant, and releases its own attachment on `Close`. Board
checks that requested capabilities were advertised and gates each invocation
against the grant. The host supplies authorization before opening a session.

```go
registry, err := board.NewRegistry(myKeyboardProvider, myDriveProvider)
if err != nil { /* handle registration error */ }

connection, err := registry.Open(ctx, board.OpenRequest{
    ProviderID: "local-drives",
    DeviceID:   "usb-1",
    Grant: board.Grant{
        Capabilities: []board.Capability{board.DriveList, board.DriveRead},
        ResourceIDs:  []string{"approved-photos-root"},
    },
})
if err != nil { /* handle unavailable or denied device */ }
defer connection.Close(ctx)

// Contents come from a bounded stream, never from a result.
content, err := connection.Stream(ctx, board.Action{
    Capability: board.DriveRead,
    ResourceID: "approved-photos-root",
    Input:      json.RawMessage(`{"path":"DCIM/100CANON/IMG_0001.JPG"}`),
})
if err != nil { /* handle a refused or unavailable path */ }
defer content.Close()
```

Registering the mounted-drive provider takes a host-approved root:

```go
drive, err := board.NewMountedDriveProvider("mounted-drive-photos", board.MountedDriveOptions{
    Root:           "/Volumes/NOAA",
    Name:           "Field Camera USB",
    RootID:         "photos",
    MaxReadBytes:   16 << 20,
    MaxListEntries: 500,
    MaxScanEntries: 2000,
})
if err != nil { /* the approved root is missing or unusable */ }

registry, err := board.NewRegistry(drive)
```

## macOS keyboard provider

`macoskeyboard.New` requires a positive target PID chosen by the caller and
macOS Accessibility permission for the calling process. It records the
target process start identity and rejects actions if that process exits or its
PID is reused. `keyboard.press` accepts named keys or modifiers plus an ANSI
letter, such as `Command+O`. `keyboard.type` accepts bounded Unicode text
without control characters. Its result `{"submitted":true}` means the events
were posted; the target application may still ignore them. This provider needs
macOS with cgo; other builds return `ErrUnsupported`.

```go
keyboard, err := macoskeyboard.New(macoskeyboard.Options{
    ProviderID: "keyboard-import", PID: targetPID,
})
if err != nil { /* handle permission or unavailable target */ }
registry, err := board.NewRegistry(keyboard)
```

The Board CLI exposes `board keyboard-press --pid PID --key Command+O`
and `board keyboard-type --pid PID` (text from stdin). `board/host` also
accepts caller-owned providers with `host.NewRegistered`, and its
`Attach` method requires an explicit `Authorizer` for every grant.

## Lifecycle and errors

Providers are registered explicitly. List, open, invoke or stream, then close
the connection. Close a mounted-drive provider after its connections to
release its pinned directory handle. On `ErrDriveUnavailable` or
`macoskeyboard.ErrTarget`, the host should select and approve a current target
before creating a new provider. `ErrNotGranted` and `ErrClosed` let callers
distinguish grant and lifecycle failures. Board does not currently push device
arrival or removal events; hosts can list again when needed.

Board is its own Go module (`module board`) and imports neither
Cymonkey nor Jangolova nor Blockade; a dependency-graph test enforces that. Run
`go test ./...` from this directory.
