# Lens Studio Cymonkey module

`lensstudiocymonkey` is a narrow Jangolova backend for a Lens Studio project
whose owner has already enabled Lens Studio's Developer Mode MCP server. It is
not an agent and it does not start Lens Studio, create a second MCP server, or
accept arbitrary Lens Studio tool names from a caller.

The composition is deliberately one-way:

```text
external agent -> Cymonkey policy / approval -> Jangolova Adapter
               -> lens-studio-mcp backend -> Lens Studio's existing MCP endpoint
```

An external agent chooses intent. Cymonkey remains responsible for target
selection, credentials, approval, and audit. This module only maps a small,
reviewable semantic surface to the Lens Studio MCP tools that are advertised by
the attached editor.

## Current capability surface

| Semantic action | Lens Studio MCP tool | Bound |
| --- | --- | --- |
| `project.describe` | `GetLensStudioSceneGraph`, optionally `ListLensStudioAssets` | Read-only. Returns only explicitly registered project, scene, and configured scene-object resources. |
| `scene.list` | `GetLensStudioSceneGraph` | Read-only. |
| `scene.property.set` | `SetLensStudioProperty` | Requires exact `allowedSceneObjectIds` and `allowedPropertyPaths` matches, plus a `previousValue` that is returned as a rollback request. |
| `preview.run-and-collect-logs` | `RunAndCollectLogsTool` | Available only when Lens Studio advertises it. This is log evidence, not screenshot capture. |

The module discovers the exact tool names from Lens Studio and supports the
published names plus a few compatibility aliases. It refuses to connect if a
scene graph or scene-property tool is absent. There is intentionally no raw
`tools/call` action.

## Registering the backend

The runtime owner registers the package explicitly; importing it is not enough:

```go
import (
    jangolova "cymonkey/lib/jangolova"
    lensstudio "cymonkey/pkg/lens-studio-cymonkey"
)

adapter := jangolova.Adapter{
    Host: host, // host validates target endpoints and supplies credential headers
    Backends: []jangolova.Backend{lensstudio.Backend{}},
}
```

Attach a caller-owned target with `kind: "lens-studio"` and an endpoint whose
protocol is `"mcp-streamable-http"`. Lens Studio presents a local endpoint and
bearer token after you configure its MCP server in Developer Mode. Keep the
token in the host's credential material (`TargetEndpoint.Connection`) so it is
rotated and never persisted by this module. `bearerTokenEnv` exists only as a
local-development fallback.

```json
{
  "adapter": "jangolova",
  "requiredCapabilities": ["scene.list", "scene.property.set"],
  "options": {
    "requestTimeout": "30s",
    "allowedSceneObjectIds": ["scene-root"],
    "allowedPropertyPaths": ["localTransform.position"]
  }
}
```

For an edit, `scene.property.set` requires this shape:

```json
{
  "name": "scene.property.set",
  "input": {
    "sceneObjectId": "scene-root",
    "propertyPath": "localTransform.position",
    "valueType": "vector3",
    "value": {"x": 1, "y": 2, "z": 3},
    "previousValue": {"x": 0, "y": 0, "z": 0}
  }
}
```

The result includes a `rollback` action constructed from `previousValue`. The
normal Cymonkey approval layer should separately require approval for this
write and for preview execution.

## Security boundaries

- The host validates the endpoint at connect time and before every MCP request.
- Target-owned connection headers are snapshotted on every request, enabling
  host-side credential rotation. The module does not write tokens to disk.
- Plain HTTP is restricted to loopback addresses; a remote target must use
  HTTPS.
- Every mutable property is subject to two exact allowlists. Empty allowlists
  deny all property edits.
- The module avoids screenshot / visual claims. A future Blockade capture or
  inference step should remain a separately registered composition above this
  backend.

## Verification

Run the deterministic MCP fixture tests:

```sh
go test ./pkg/lens-studio-cymonkey
```

The fixture covers the Streamable HTTP initialization sequence, session reuse,
target-supplied headers, the named tool mappings, explicit resource
description, rollback, and denied writes. It is not a live Lens Studio test.

For live validation, open a Lens Studio project, enable Developer Mode's MCP
server, copy the generated MCP configuration into the caller-owned target, and
then test in this order:

1. `project.describe` and `scene.list` against a disposable project.
2. One allowlisted property change with a known `previousValue`, followed by
   the returned rollback action.
3. `preview.run-and-collect-logs`, confirming the result contains editor logs
   only.

The local environment used for this change does not include Lens Studio, so
the live-editor portion remains to be run by a developer with the editor open.

References: [Lens Studio plugins overview](https://developers.snap.com/lens-studio/extending-lens-studio/plugins-development/overview), [Lens Studio Developer Mode MCP](https://developers.snap.com/lens-studio/features/lens-studio-ai/developer-mode), and [the documented MCP custom-prompt tools](https://developers.snap.com/lens-studio/features/lens-studio-ai/developer-mode/custom-prompt-for-mcp).
