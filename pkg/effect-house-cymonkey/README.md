# Effect House Cymonkey module

`effecthousecymonkey` is a guarded source-project adapter for an already-open
TikTok Effect House project. It complements Effect House's native **Ask AI**
integration with Codex; it does not impersonate the editor, create visual
graphs, attach components, operate Preview, or publish an effect.

Effect House itself remains the owner of those actions. This module controls a
smaller, auditable boundary: named APJS TypeScript component sources that the
project owner explicitly registers.

```text
external agent -> Cymonkey approval / audit -> Jangolova Adapter
               -> effect-house-project-files -> registered APJS .ts files

Effect House Ask AI / editor -> imports, attaches, compiles, previews, publishes
```

## Why this is the right bridge

Effect House documents APJS as TypeScript component scripts using `@component()`
and `APJS.BasicScriptComponent`; normal editing occurs through an external code
editor. Its newer Ask AI panel can also connect directly to Codex and work in
the open project. Neither document describes a public external editor-control
or MCP server comparable to Lens Studio's Developer Mode MCP.

So this module does not invent one. It makes project-file cooperation bounded:

| Semantic action | Effect |
| --- | --- |
| `project.describe` | Lists only the attached project and registered script resources, with paths and content digests. |
| `script.list` | Lists registered script IDs, paths, sizes, and SHA-256 digests—never discovers the project tree. |
| `script.read` | Reads one registered script. |
| `script.validate` | Checks that candidate source has the minimum documented APJS component shape without writing it. |
| `script.replace` | Atomically writes one writable registered script only if `expectedSha256` matches the current file; returns a rollback request. |

No raw path, arbitrary file, shell command, visual-scripting graph, scene
object, asset import, preview, or publication action exists in this module.

## Registering a project

The host mounts this backend explicitly:

```go
import (
    jangolova "jangolova"
    effecthouse "cymonkey/pkg/effect-house-cymonkey"
)

adapter := jangolova.Adapter{
    Host: host, // validates caller-owned project roots and enforces approvals
    Backends: []jangolova.Backend{effecthouse.Backend{}},
}
```

Use a caller-owned target of kind `"effect-house"` with a
`"local-project"` endpoint whose URL is an absolute project directory. The
host must validate that root before attachment and each semantic call.

```json
{
  "adapter": "jangolova",
  "requiredCapabilities": ["script.read", "script.replace"],
  "options": {
    "allowWrite": true,
    "registeredScripts": [
      {"id": "tap-counter", "path": "scripts/TapCounter.ts", "writable": true},
      {"id": "theme", "path": "scripts/Theme.ts"}
    ]
  }
}
```

Registered script paths must be relative `.ts` paths below the project root;
their file names use Effect House's documented letters/digits/underscore rule.
The module resolves symlinks and rejects any resource that escapes the root.
It does not create script resources: create and attach the component through
Effect House first, then register its existing source path.

## Safe edit flow

1. Call `script.read` and retain its returned `sha256`.
2. Optionally call `script.validate` with `{scriptId, source}`.
3. Seek the normal Cymonkey approval for `script.replace`.
4. Send the exact expected digest and candidate APJS source.
5. Review / test the changed script in Effect House, or send the returned
   rollback action.

```json
{
  "name": "script.replace",
  "input": {
    "scriptId": "tap-counter",
    "expectedSha256": "<digest-from-script.read>",
    "source": "@component()\nexport class TapCounter extends APJS.BasicScriptComponent { /* ... */ }"
  }
}
```

The replacement is written to a same-directory temporary file and renamed into
place only after the digest check. The response includes the prior source and
the new digest as an exact rollback request. A concurrent external edit causes
the replacement to fail rather than overwrite it.

## Verification and limits

Run the deterministic fixture:

```sh
go test ./pkg/effect-house-cymonkey
```

It verifies contract conformance, explicit resource visibility, stale-write
rejection, atomic replacement/rollback, read-only enforcement, and a symlink
escape rejection. It does not launch Effect House; live validation requires an
open project, an existing component script, and a manual or native Ask AI
Preview/Console check.

This module is intentionally complementary to Effect House Ask AI. Ask AI can
create scenes, scripts, and assets and may capture Preview screenshots under
the editor's own consent settings. Visual scripting is out of scope because
Effect House says Ask AI generates APJS scripts rather than visual-scripting
graphs. Blockade-based visual evidence should be composed separately above this
source adapter.

References: [Effect House Ask AI overview](https://effecthouse.tiktok.com/learn/guides/ask-ai/ask-ai-overview), [APJS scripting guide](https://effecthouse.tiktok.com/learn/guides/api-reference/scripting-capability-usage-guide), and [visual scripting overview](https://effecthouse.tiktok.com/learn/guides/editor-panels/visual-scripting).
