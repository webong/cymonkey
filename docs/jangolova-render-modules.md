# Jangolova render modules

Jangolova owns the engine-specific render contracts and runtime libraries.
Cymonkey is the host and coordination layer: it discovers approved modules,
verifies and caches selected artifacts, mounts them into caller-owned targets,
applies policy, and routes calls.

```text
Cymonkey host/coordinator
        |
        +-- Jangolova render/threejs
        +-- Jangolova render/godot
        +-- Jangolova render/unity
        +-- Jangolova render/unreal
        +-- Jangolova render/snapchat-camera-kit
        +-- Jangolova render/blender
```

Every module owns its engine-facing implementation and advertises the common
`hello`, `capabilities`, `describe`, `act`, `events`, and `health` contract. The
engine still owns its scene, renderer, and process lifecycle; Jangolova never
launches or terminates the caller-owned target.

## Canonical module names

The stable public names are module IDs, not repository directory names:

| Module ID | Runtime | State | Current distribution path |
| --- | --- | --- | --- |
| `render/browser-dom` | Browser document/augmentation | implemented | `pkg/browser-ext` |
| `render/threejs` | Three.js | implemented | `pkg/threejs-cymonkey` |
| `render/godot` | Godot 4 | implemented | `pkg/godot-cymonkey` |
| `render/unity` | Unity | implemented | `pkg/unity-cymonkey` |
| `render/unreal` | Unreal Engine | implemented | `pkg/unreal-cymonkey` |
| `render/snapchat-camera-kit` | Snap Camera Kit Web | implemented | `pkg/snapchat-camera-kit-cymonkey` |
| `render/blender` | Blender | implemented | `pkg/blender` |

The `*-cymonkey` directories are compatibility source paths retained while
existing projects migrate. New configuration refers to canonical module IDs,
never to repository paths. The Jangolova registry publishes versioned module
metadata and optional platform artifacts; Cymonkey does not treat a source
directory as an installed plugin.

The checked-in reference snapshot is
[`lib/jangolova/registry/index.json`](../lib/jangolova/registry/index.json). Production
hosts should call the registry discovery client with a configured HTTPS
endpoint, then pull only the selected artifact after policy approval.

## Discovery and pulling

Discovery is metadata-only. A registry entry declares the module ID, version,
runtime, protocol version, supported platforms, actions, and (when published)
immutable artifacts with SHA-256 digests. Cymonkey verifies the registry
shape, selects a compatible entry, downloads into its cache, and verifies the
artifact digest before exposing the path to a provider. It never executes
downloaded code implicitly.

Browser packages follow the same rule: the extension may mount only a reviewed
package present in its signed product registry. Native render packages are
pulled by the Cymonkey host/provider for the target machine, not by the
browser extension.

## Ownership boundaries

- Jangolova modules validate registrations, allowlists, schemas, actions,
  events, transport authentication, and runtime-specific calls.
- Cymonkey discovers, verifies, caches, and coordinates approved modules and
  their attachments; it does not own their engine semantics.
- The target provider owns Godot, Unity, Unreal, Blender, or browser process
  lifecycle, displays, capture, video transport, and credentials.

WebRTC, VNC, RDP, and similar streams are viewer transports. They are not part
of a render module's semantic contract.
