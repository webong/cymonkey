# Board

Board is the standalone input and output device library for Cymonkey. It
provides a small contract for discovering devices, opening a host-approved
session, and invoking bounded capabilities. A Board provider binds that
contract to a particular operating system, device class, or transport.

Board currently contains the provider and grant contract. It has **no native
keyboard or drive provider yet** and does not access devices by itself.

## First workflow: import photos from a USB drive

1. A registered drive provider reports the USB drive and its capabilities.
2. The host approves `drive.list` and `drive.read` for a selected resource
   root. The drive provider resolves that opaque root and confines all listing
   and reading to it.
3. A keyboard provider can separately receive a grant for `keyboard.press`
   or `keyboard.type` to operate an already open import dialog.
4. The host coordinates the file selection and app interaction. A future
   Cymonkey binding can expose each session through its semantic capability
   contract.

The first capability names are `keyboard.press`, `keyboard.type`, `drive.list`,
and `drive.read`. Providers may define more names for other devices. A grant
names one provider, one device, and a subset of that device's advertised
capabilities. Drive grants also require opaque root IDs. Each drive action
must name one granted root; the provider must confine any path inside the
action input beneath that root, including aliases and links. Board cannot
resolve provider-specific resources itself.

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
        Capabilities: []board.Capability{board.DriveList},
        ResourceIDs:  []string{"approved-photos-root"},
    },
})
if err != nil { /* handle unavailable or denied device */ }
defer connection.Close(ctx)
```

Providers may return structured JSON for small results. A production drive
provider should define a streaming read interface and size limits before
exposing file contents; `Result.Output` is not intended to carry large files.
The Cymonkey protocol binding, platform providers, hotplug events, and
streaming reads are next steps.

Board is its own Go module (`module board`) and imports neither Cymonkey nor
Jangolova nor Blockade. Run `go test ./...` from this directory.
