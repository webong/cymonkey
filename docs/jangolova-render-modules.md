# Jangolova render modules

Jangolova owns the engine-specific render contracts and runtime libraries.
Cymonkey is the host and coordination layer: it selects approved modules,
mounts them into caller-owned targets, applies policy, and routes calls.

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

The `*-cymonkey` directories are compatibility distribution paths retained
while existing projects migrate. New manifests and host configuration should
refer to the canonical module IDs above. A physical directory move is a
separate, versioned packaging change; it must not silently replace the
existing `pkg/unity` Jangolova Bridge package.

The machine-readable source of this mapping is
[`pkg/jangolova-render-modules.json`](../pkg/jangolova-render-modules.json).

## Ownership boundaries

- Jangolova modules validate registrations, allowlists, schemas, actions,
  events, transport authentication, and runtime-specific calls.
- Cymonkey chooses which compiled modules are available and coordinates their
  attachments and policy.
- The target provider owns Godot, Unity, Unreal, Blender, or browser process
  lifecycle, displays, capture, video transport, and credentials.

WebRTC, VNC, RDP, and similar streams are viewer transports. They are not part
of a render module's semantic contract.
