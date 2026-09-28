# Cymonkey, Jangolova, Blockade, and Board boundaries

Jangolova provides display interfaces. Blockade provides inference interfaces
for image and sound across local and cloud backends. Board defines input and
output device interfaces. Cymonkey is the operator, entry, and extension layer
that composes these modules. Blockade's current public request is image-based;
sound inference has not been implemented. Board ships a read-only mounted-drive
provider and a macOS keyboard provider, but no hotplug events yet. The libraries are
standalone: Cymonkey depends on their public APIs; none depends on Cymonkey or
another domain library. All three may use the shared executable plugin transport.

```text
caller or agent
  → Cymonkey authorizes and coordinates extensions to the system
    → Jangolova operates or presents on caller-owned displays and runtimes
    → Blockade runs local or cloud inference and returns evidence
    → Board binds host-approved input and output devices
```

## Cymonkey

Cymonkey discovers, installs, mounts, and coordinates access to a runtime. In
a browser it provides libraries and interfaces for caller-owned extensions,
reviewed packages, target-tab selection, approval, and authenticated routing.
Its executable registers Jangolova and Blockade adapters through their public
Go APIs; it can also host provider processes. It does not implement
scene semantics, inspect arbitrary Three.js objects, or embed inference engines.

## Jangolova display interfaces

Jangolova owns the libraries and adapters that run *inside* a target runtime:
Three.js, Godot, Unity, Unreal, Blender, browser interaction, and future presentation
hosts. A library registers stable resources and an allowlist of semantic
operations through the shared bridge contract. It owns the translation from a
semantic request to a safe runtime-native operation.

A Jangolova runtime library can also be used without Cymonkey when a caller
already has a suitable runtime endpoint. Cymonkey is the normal way to enter
and coordinate that runtime, not a requirement baked into the library.

## Blockade inference interfaces

Blockade owns local and cloud inference integrations. Its current image request
receives pixels and returns normalized observations. Sound is part of its
inference scope but needs a separate public contract. Cymonkey may coordinate
a Jangolova screenshot with Blockade, but neither Jangolova nor Blockade
imports or configures the other.

Provider APIs and VLMs are Blockade adapter integrations, not local engines.
Blockade owns their configuration, credentials, and normalization into its
observation contract, while Cymonkey exposes the coordinated
screenshot-to-observation capability. This keeps Blockade deployable as a
standalone inference service and Jangolova limited to runtime interaction and
presentation libraries.

## Board device interfaces

Board providers bind concrete keyboard, drive, and future device APIs to a
host-approved capability grant. The host selects a provider, device, and
capabilities. A drive provider also receives opaque resource roots and must
enforce containment for each listing and read. Board's registry gates actions
to the grant; the provider validates its device-specific input and resource
scope. Board does not own the device lifecycle.

Board's mounted-drive provider implements that contract for an already-mounted
directory. It is read-only, takes the approved root from the host rather than
discovering one, and confines every action through `os.Root`: absolute paths,
paths escaping through `..`, and links that leave the root are refused. Links
staying within the root work. Inaccessible entries are withheld from listings
and counted. File contents come from a bounded content stream, not a `Result`.
The provider checks the approved directory's identity on every new action, so
replacing a mount at the same path requires new host approval. `os.Root` does
not block traversal into a mount nested within the approved directory.

The macOS keyboard provider sends bounded key or Unicode events to a caller
selected PID, with Accessibility permission and process identity checks. It
reports event submission, not confirmation that an application used the input.

`board/host` registers host-approved roots or caller-owned providers, then
turns a request into a grant. The caller must supply an `Authorizer`; a
decision may narrow a request and never widen it. `board/cli` exposes drive
discovery, listing, reading, and macOS keyboard actions. Cymonkey routes its
top-level `board` command to that library CLI. Hotplug events and a persistent
device protocol remain to be implemented.
