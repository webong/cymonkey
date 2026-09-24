# Web presentation provider handoff

`web-presentation` is the first provider-visible presentation adapter in
Jangolova. It lets an agent create and update an interface without making
Jangolova responsible for the browser or display process.

## Ownership

Jangolova owns the declarative document model, semantic actions, presentation
events, artifact contract, and the adapter worker. The caller's target
provider—Xallet, a native host, or a direct-container supervisor—owns the
static asset server, Chromium process, CDP endpoint, display/window placement,
GPU, and cleanup. Disconnecting the Jangolova instance must leave that target
running.

## Target contract

The target provider starts a Chromium-compatible browser and supplies a CDP
endpoint. The provider may host the reference page in
`examples/web-presentation` or provide another page implementing the same
`window.cymonkey.jangolova` bridge:

```json
{
  "engine": {
    "adapter": "web-presentation",
    "source": "https://presentation.example/session.html",
    "options": {
      "policy": {
        "maxHTMLBytes": 1048576,
        "maxCSSBytes": 262144,
        "maxJavaScriptBytes": 262144,
        "maxTotalBytes": 1572864,
        "allowedSourceOrigins": ["https://presentation.example"],
        "allowedAssetOrigins": ["self", "https://assets.example"],
        "allowedArtifactTransports": ["http", "https"],
        "authorizedActions": ["presentation.capture", "presentation.execute", "presentation.mount"],
        "executeTimeoutMillis": 5000,
        "captureTimeoutMillis": 10000,
        "mountTimeoutMillis": 15000
      }
    }
  },
  "target": {
    "kind": "browser",
    "endpoints": [{
      "name": "cdp",
      "protocol": "cdp",
      "url": "http://127.0.0.1:9222"
    }]
  }
}
```

`source` is optional when the target provider has already opened a compatible
page. Jangolova attaches with Puppeteer Core and never calls a browser launch
or shutdown API.

## Artifact policy

The artifact model supports inline `{html, css, js}`, structured presentation
documents, and versioned artifact references defined by the
[provider-neutral schema](../src/protocol/presentation/v1/artifact.schema.json).
Artifact bytes do not pass through Jangolova. The attached engine loads one of
the caller-supplied locations directly.

Every `web-presentation` connection has an enforced policy. Defaults are:

- HTML: 1 MiB;
- CSS: 256 KiB;
- authored or executed JavaScript: 256 KiB;
- total inline source or structured document: 1.5 MiB;
- source page: unrestricted unless `allowedSourceOrigins` is configured;
- loaded assets: `self`, `data:`, and `blob:`;
- artifact transports: `http` and `https`; `target-file` can be enabled for
  a target-visible mounted filesystem;
- sensitive actions authorized by policy: `presentation.capture`,
  `presentation.execute`, and `presentation.mount`;
- `presentation.execute` timeout: five seconds;
- `presentation.capture` timeout: ten seconds;
- `presentation.mount` timeout: fifteen seconds.

Limits count UTF-8 bytes. Each configured limit must be between one byte and
32 MiB. Origins must be exact HTTP(S) origins without paths, queries,
fragments, or credentials. When `allowedSourceOrigins` is present, the
configured `source` must match it. `allowedAssetOrigins` accepts exact HTTP(S)
origins plus `self`, `data:`, and `blob:`. Supplying an asset list replaces the
default list.

`authorizedActions` is the provider authorization gate for sensitive
presentation operations. It accepts `presentation.execute`,
`presentation.capture`, and `presentation.mount`; supplying an empty list
denies all three. These actions also
emit provider instance audit events named
`presentation.execute.requested`, `.succeeded`, `.failed`, `.cancelled`, or
`.denied`, and the equivalent `presentation.capture.*` names. Audit events do
not include script source, screenshots, or result payloads.

`executeTimeoutMillis`, `captureTimeoutMillis`, and `mountTimeoutMillis` must
be between 1 and 120000. The worker enforces these deadlines inside the CDP
attachment. On an execute timeout it sends Chrome
`Runtime.terminateExecution`; on a mount timeout it stops page loading. The
adapter also keeps a slightly longer cancellation deadline around the worker
request.

The worker enforces asset origins through Chromium request interception. This
is defense in depth for the authored surface, not a replacement for Xallet's
container or host network policy.

## Semantic operations

Call the normal bridge `act` method with one of these names:

- `presentation.create` — initialize a document.
- `presentation.replace` — replace the complete document.
- `presentation.write` — write an HTML/CSS/JavaScript presentation artifact.
- `presentation.mount` — load a versioned artifact through an engine-supported
  caller-supplied location.
- `presentation.execute` — run presentation JavaScript against the mounted
  surface when an incremental behavior update is needed.
- `presentation.patch` — apply `set`, `remove`, or `append` operations.
- `presentation.describe` — return the document and viewport.
- `presentation.activate` — activate a semantic button by id.
- `presentation.capture` — return a PNG captured by the attached browser.

Use the bridge `events` method with a cursor to consume activation, update, and
resize events. A richer Three.js, Unity, or Unreal host can implement the same
bridge and expose engine-specific capabilities alongside these operations.

For example, an agent can write a complete surface with:

```json
{
  "name": "presentation.write",
  "input": {
    "expectedStateRevision": "0",
    "html": "<article id=\"card\"><h1>Hello</h1><button id=\"next\">Next</button></article>",
    "css": "#card { padding: 32px; }",
    "js": "root.querySelector('#next').onclick = () => emit('next.clicked', {});"
  }
}
```

`presentation.create`, `presentation.replace`, `presentation.write`, and
`presentation.patch` require `expectedStateRevision`. A new page begins at
state revision `"0"`; every successful mutation returns and emits the next
state revision. Mutation receipts do not echo the complete document.
`presentation.describe` returns the current state revision. A stale mutation
fails with a conflict and leaves the current document unchanged.

`presentation.create` is valid only at the empty revision. Use
`presentation.replace` or `presentation.patch` for subsequent updates.

Mount a large caller-hosted experience without sending its bytes through the
provider:

```json
{
  "name": "presentation.mount",
  "input": {
    "expectedStateRevision": "3",
    "artifact": {
      "apiVersion": "interaction.presentation/v1alpha1",
      "artifactId": "experience-42",
      "revision": "sha256:abc123",
      "kind": "web.entrypoint",
      "locations": [
        {"transport": "https", "uri": "https://assets.example/experience/index.html"},
        {"transport": "target-file", "uri": "file:///presentations/experience/index.html", "audience": "target"}
      ]
    }
  }
}
```

The engine chooses the first supported location allowed by connection policy.
The mount receipt contains `artifactId`, immutable `artifactRevision`, the new
page's `stateRevision`, and the selected transport—but not artifact contents.

The JavaScript runs inside the caller-owned browser page and is therefore an
explicit externally-effectful capability. The calling agent system remains
responsible for deciding which sessions receive `presentation.execute`
authorization. Jangolova enforces the provider-supplied authorization policy,
records audit events, and bounds execution time.

## Target-provider implementation notes

Xallet can model the CDP endpoint and artifact locations as target resources,
translate container addresses, and keep them private to the session network.
A native host or direct-container supervisor can instead supply localhost URLs
or target-visible file locations. None of these choices changes the Jangolova
provider contract. The same artifact descriptor can later be interpreted by a
Three.js, Unity, or Unreal presentation engine.
