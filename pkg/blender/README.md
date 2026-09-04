# Jangolova render/blender module

This package is Jangolova's native Blender render integration. Cymonkey mounts
and routes the module; Blender owns the scene, renderer, and process lifecycle.

The module is a small, dependency-free Python bridge intended to run from
Blender's bundled Python interpreter. It exposes only explicitly registered
scene resources over the common `cymonkey/v1alpha1` contract:

```text
hello → capabilities → describe → act → events → health
```

Supported actions:

- `resource.describe`
- `object.visibility.set`
- `object.transform.set`
- `material.color.set`
- `camera.transform.set`
- `render.frame`

The package never scans `bpy.data`, the active scene, or the Blender context
to discover resources. A caller registers each resource with a stable ID and a
per-resource action allowlist.

## Blender usage

Run Blender headlessly with a caller-owned scene and bootstrap script:

```sh
blender --background scene.blend --python bootstrap_cymonkey.py
```

`bootstrap_cymonkey.py` creates a `CymonkeyRegistry`, registers selected
objects/materials/cameras, and starts `CymonkeyWebSocketHost` with the token
from `JANGOLOVA_CYMONKEY_TOKEN`. The host accepts either an
`Authorization: Bearer <token>` WebSocket upgrade or a first-frame auth
message of the form `{"type":"auth","token":"..."}`.

The WebSocket server runs its socket loop off the Blender main thread and
dispatches queued requests through a Blender timer, so `bpy` mutations and
renders remain on Blender's main thread. Closing the transport only detaches
Jangolova; it does not quit Blender or free the scene.

## Distribution

The canonical module ID is `render/blender`. The current source distribution
path is `pkg/blender`; see the
[Jangolova render module map](../../docs/jangolova-render-modules.md).
