# Tachikoma and Peekaboo feature extraction

This document maps the reusable model, tool and desktop features into Cymonkey.
The SDK contracts live in libraries, native implementations and API dialects
live in optional packages, and CLI configuration and serving live in `src`.
Console remains a tool composition library for humans and agents.

The source audit uses [Tachikoma](https://github.com/openclaw/Tachikoma/tree/b22d373e5824afb7839e3b38a75b03808c3fb4d4)
and [Peekaboo](https://github.com/openclaw/Peekaboo/tree/904e5774d06f9731e0d67f6dc189af12ca0e0d1f)
at those pinned revisions. The tables describe source implementation, not live
model or desktop acceptance. Full upstream feature parity is not claimed.

## Ownership

| Location | Responsibility |
| --- | --- |
| `lib/jangolova/generationkit` | Neutral messages, generation, streaming, embeddings, speech, validation and explicit model registration |
| `pkg/console` | Schema-checked tools, selected invocation, one model turn, transcript continuation, bindings and HTTP/MCP presentation |
| `lib/jangolova/displaykit/ui` | Portable UI observations, target identity, action receipts, predicates and bounded read-only waits |
| `pkg/console/capture` | Finite image processing, native capture bridge, frame sampling, manifests and caller-supplied video decoding |
| `pkg/board/clipboard` | Clipboard snapshots, conditional writes and Board resource grants |
| `pkg/board/audio` | Device interfaces and finite PCM16/WAV conversion |
| `github.com/webong/ext/pkg/plugin/jsonschema` | Shared bounded offline schema validation (ext) |
| `pkg/content-endpoints` | Explicitly selected HTTP/WebSocket protocols, credentials and native model options |
| `lib/macos-helper` | Native AX, capture, targeted input and clipboard implementation |
| `src/console_command.go` | Host configuration, borrowed operator resources, CLI, listener/authentication and artifact destinations |

Blockade has no built-in providers, endpoint defaults, model catalog or
credential lookup. Provider and model identifiers remain opaque. Console has
no agent loop, planner, automatic retry, provider fallback or implicit tool
execution. No library binding launches a browser, app, MCP process or device.

## Model features

| Feature | Implementation and limits |
| --- | --- |
| Generation, multimodal messages and function proposals | Existing Blockade contracts extended with reasoning, refusals and HTTPS/file media references. Media is passed to the chosen adapter; the SDK never fetches it. |
| Streaming | Native synchronous callbacks, text/reasoning/refusal/tool deltas, completed proposals, usage and final response. No simulated stream. |
| Structured generation | Completed output is validated against the supplied schema. Refusal, truncated output and tool proposals are not decoded as objects. |
| Structured streaming | `StreamStructuredOutput[T]` emits raw text, bounded complete schema-valid candidates and a validated final object. It does not repair incomplete JSON. |
| Native model controls | Explicit adapter options for reasoning, thinking, sampling, tool choice and supported verbosity controls. Unknown controls fail. No model-name guessing. |
| Embeddings | Text/token inputs, dimension checks, bounded concurrent batches, explicit vector normalization and cosine similarity. Optional OpenAI-compatible, Cohere and Voyage mappings. |
| Usage and errors | Neutral input/output/cached/reasoning token counts and typed error categories. Retry information is advisory; no automatic retry. |
| Transcription | Finite audio input with optional language, duration, segment and word timing. Unsupported endpoint/model options fail. |
| Speech synthesis | Existing explicit synthesizer/consumer binding; CLI can store bounded audio in one configured existing directory. No automatic playback. |
| Realtime audio | Caller-controlled send, commit, respond, interrupt, input clear and played-audio truncation. Normalized events and a pure state reducer. No microphone or tool execution is implicit. |
| Tool composition | Definitions, schema checks, explicit results including failure flags, proposal inspection and copied continuation transcripts. |
| MCP | Explicit HTTP or supplied stdio attachment, bounded tool discovery/calls, resource/template/prompt discovery and selected reads. Tools can be published over stateless JSON POST or borrowed JSON-lines streams. |

The endpoint package supports Chat Completions, Responses, Anthropic Messages
and Gemini Generate Content. Compatible servers and local model hosts can use
the applicable protocol with their own configured API root. Protocol support
does not establish every model's capabilities. Hosted built-in tools, uploads,
image generation and unmapped native output blocks are unsupported.

MCP resources and prompt messages are returned as inert native JSON. They are
not automatically copied into a model request. Background subscriptions,
sampling callbacks and automatic reconnection are not implemented.

## Desktop features

| Feature | Implementation and limits |
| --- | --- |
| Fresh semantic observations | Native `ui.observe` returns opaque IDs, process generation, producer ID, optional exact window, bounds, attributes, expiry and traversal completeness. |
| Combined observation | Console `bindings.See` combines native pixels and semantics only when process generation and window agree. Reads are sequential, not atomic. |
| Semantic action and set-value | Observation-bound action receipts; native references must belong to the originating observation and still be fresh. Only native advertised/allowlisted actions and attributes are writable. |
| Window geometry | Explicit `AXPosition` and `AXSize` writes with typed values and immediate native readback. There is no claim that window geometry has settled; a separate wait can check state. |
| Text selection | Explicit allowlisted `AXSelectedTextRange` writes use UTF-16 offsets and return readback. Unsupported controls fail. |
| Menu, menu bar, dock and dialogs | Existing AX query/action primitives can operate exposed elements under explicit app grants. Dedicated upstream command conveniences and private platform behaviors are not implemented. |
| Pointer and keyboard | Native input requires a fresh capture, pins process/window/geometry, converts image pixels to desktop points and targets the PID. Keyboard delivery requires the selected window to be the application's focused window. No implicit focus or global-input fallback. |
| Verification and waits | `exists`, `absent` and attribute `equals`; partial observations cannot establish absence. Waits pin identity and perform only bounded reads. |
| Clipboard | Complete bounded multi-format snapshots; conditional write/clear and explicit restoration. Clipboard resource grants are independent of app grants. macOS change-count checks are not atomic against other processes. |
| Capture and evidence | PNG digest/identity/freshness checks; before/after evidence around one caller-selected operation; crop, element-box annotations, contact sheets, change/heartbeat sampling and manifests. |
| Video sampling | Explicit offsets through a borrowed decoder, with duration/frame/pixel/byte limits and host-owned storage. No bundled codec, video-path lookup or encoder process is launched. |
| Audio/device access | Existing Board sessions/grants/channels remain the device boundary; finite PCM16/WAV helpers bridge owned audio bytes to models. |
| Browser automation | Existing Jangolova CDP, BiDi, WebDriver, augmentation and mapped MCP backends remain reusable through borrowed callers. |

Native observations expire after 15 seconds and report partial traversal when
limits or AX failures prevent a complete answer. Exact window resolution uses
live CG identity and an unambiguous AX geometry match. Public macOS APIs do not
provide a stronger window incarnation token; unavailable or ambiguous targets
fail. Foreign AX calls use bounded messaging timeouts, and AX access to the
embedding process is dispatched on the main queue.

Native captures are bounded to 16 million pixels and 2 MiB encoded PNG bytes.
Jangolova policy must explicitly select `allowedViewerSurfaces` for captured
viewer actions and `allowedClipboardResources` for clipboard operations.
The helper also needs its native viewer/clipboard policies and OS consent.
Board clipboard action JSON is limited to 64 KiB by its existing action decoder;
larger reads use a finite content stream. A 16 MiB clipboard SDK ceiling is not
a promise of 16 MiB native or Board action payload support.

Transport cancellation does not prove a native mutation was canceled. After a
request is sent, a failed call may have unknown dispatch. Receipts describe
dispatch separately from verification; the caller decides what evidence to
collect before another mutation.

## Using the composition host

See [Console configuration](console.md) and the
[example configuration](../pkg/console/examples/operator/config.json).
The CLI selects models, existing display instances, existing granted device
sessions and named MCP tools. It does not attach new targets.

```text
human or external agent
  -> Console definitions and explicit invocation
     -> Blockade selected model -> optional endpoint adapter
     -> Jangolova borrowed display -> optional native/browser runtime
     -> Board borrowed grant -> selected device backend
     -> MCP selected connection -> selected tool/resource/prompt
```

Each generation performs one model turn and returns proposed calls. A host
decides which call to invoke, handles every proposal, constructs a continuation
transcript and decides whether to request another turn. Direct human tool
invocation needs no model.

## Remaining work and exclusions

These items remain explicit gaps, rather than advertised unsupported features:

- Native Spaces management, dedicated app/window/menu/dock/dialog convenience
  services, richer modifier/key sequences and guaranteed background rich paste.
- A visual feedback/menu-bar app, persistent capture store, recording encoder
  and bundled video decoder. These belong in optional packages or host products.
- Broader provider-specific operations such as hosted tools, file upload and
  image generation, plus provider-specific speech/realtime dialects.
- Live endpoint acceptance, native UI/permission acceptance and cross-platform
  conformance. Compilation alone does not establish these behaviors.

Autonomous agent sessions, automatic model/tool loops, default providers,
credential discovery and built-in model/pricing catalogs are intentionally
excluded from these libraries. Hosts may build their own workflows upon the
explicit primitives.

## Build evidence

The changed Go modules and root executable compile in the workspace. The macOS
helper compiles with Swift using task-local module/build caches. The affected Go libraries and endpoint package also compile as standalone
modules with workspace mode disabled. No tests,
live model calls, clipboard mutations, UI actions or live device sessions were
run for this change. A whole-repository Darwin `go build ./...` also reaches
the existing Windows-only helper main package, which has no Darwin entrypoint;
root executable compilation uses `./src/...`.
