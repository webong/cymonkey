# SDK model and tool composition

The reusable features from [Tachikoma](https://github.com/openclaw/Tachikoma)
are assigned to the libraries that own their contracts. Console lives in
`lib/console` and provides composition for humans and agents.

| Feature | Cymonkey SDK ownership |
| --- | --- |
| Typed generation, multimodal messages, schemas, tool proposals, usage | Blockade contracts and validation |
| Streaming text and tool deltas | Blockade streaming contract; optional adapters implement native transport |
| Endpoint and model selection | Explicit host configuration and registrations; external provider modules |
| Tool definition, typed handlers, selected invocation | Console registry |
| One model turn with advertised tools; transcript continuation | Console `ModelTools`, `ProposedCalls`, `Continue` |
| MCP tool discovery and selected calls | Console MCP attachment over supplied HTTP endpoint or stdio pipes |
| Transcription, synthesis, duplex voice | Blockade contracts; optional external speech/realtime adapters |
| Fixed-model generation and speech as callable tools | Console bindings, with explicit input ceilings and host-owned audio consumers |
| Audio capture/playback and finite channels | Board device grants and channel limits; configurable PCM rate/channels |
| Browser/native/render interaction | Existing Jangolova semantic actions, borrowed through Console bindings |

Blockade must never hardcode providers. Provider and model IDs are opaque strings;
its catalogs start empty. API dialects, provider headers, endpoint URLs, model
defaults, and credentials belong to external adapters and their embedding host.
The new endpoint SDK requires explicit URLs/models and contains no provider
profiles. Existing operator plugin configuration remains a separate surface.

## Caller-controlled handoff

1. The host selects a generator and registers tools bound to existing approved
   resources. It can show the registry to a human without using any model.
2. An explicit Console `Generate` or `Stream` advertises definitions and makes
   one model request. The result contains proposed calls as data.
3. The host inspects proposals and makes approval/selection decisions.
4. Each explicit `Registry.Invoke` executes one selected call. A host may
   supply a public failure result for a denied or failed call.
5. `Continue` validates complete, matching tool results and copies the
   transcript. The host decides whether another model turn is useful.

Console provides no autonomous multi-step loop, planner, memory store, automatic
retry, background tool execution, process launcher, or session manager. Caller
applications may build those behaviors upon the primitives if desired.

The [runnable handoff example](../lib/console/examples/handoff/README.md)
demonstrates inspection, explicit invocation, rejection, and direct human use
with a scripted local generator. No remote model or device is needed.

## Embedding surfaces

- [`lib/blockade`](../lib/blockade/README.md): neutral contracts and model bindings.
- [`lib/console`](../lib/console/README.md): tool composition, SDK bindings, MCP.
- [`lib/board`](../lib/board/README.md): existing device providers and audio channels.
- [`lib/jangolova`](../lib/jangolova/README.md): existing semantic interaction SDK.
- [`pkg/content-endpoints/generation`](../pkg/content-endpoints/generation/README.md): optional HTTP API dialects.
- [`pkg/content-endpoints/realtime`](../pkg/content-endpoints/realtime/README.md): optional explicit model WebSocket session.

This adds native Go embedding surfaces. It does not imply every Tachikoma
provider/model feature is supported or add generation/speech routes to Cymonkey's
existing observation/interpretation CLI and process-plugin contracts. Adapter
support depends on the selected endpoint/model; unmapped native features fail
explicitly.

## Extracted desktop and model features

The [extraction map](tachikoma-peekaboo-extraction.md) records pinned upstream
revisions, implementation ownership and remaining gaps. Added surfaces include
structured-output validation/streaming, embeddings, native options, UI snapshot
identity, verification/waits, capture processing and clipboard grants.
[Console configuration](console.md) covers explicit CLI, HTTP and MCP composition.
