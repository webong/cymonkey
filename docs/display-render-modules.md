# Display render modules

Cymonkey exposes engine-specific render integrations as display modules. It
discovers approved modules, verifies and caches selected artifacts, mounts
them into caller-owned targets, applies policy, and routes calls.

```text
Cymonkey display service
        |
        +-- render/threejs
        +-- render/godot
        +-- render/unity
        +-- render/unreal
        +-- render/snapchat-camera-kit
        +-- render/blender
```

Each module advertises `hello`, `capabilities`, `describe`, `act`, `events`,
and `health`. The target owner retains its scene, renderer, process lifecycle,
display, credentials, and media transport.

## Canonical module names

| Module ID | Runtime | State |
| --- | --- | --- |
| `render/browser-dom` | Browser document and augmentation | implemented |
| `render/threejs` | Three.js | implemented |
| `render/godot` | Godot 4 | implemented |
| `render/unity` | Unity | implemented |
| `render/unreal` | Unreal Engine | implemented |
| `render/snapchat-camera-kit` | Snap Camera Kit Web | implemented |
| `render/blender` | Blender | implemented |

Configuration refers to these IDs rather than repository paths. Registry
metadata declares the module version, runtime, protocol, supported platforms,
actions, and immutable artifacts. Cymonkey verifies the registry and selected
artifact before exposing a cache path to the host.

See the [display module registry](display-module-registry.md) for discovery and
pulling. WebRTC, VNC, RDP, and similar streams are display transports and are
separate from a render module's semantic contract.
