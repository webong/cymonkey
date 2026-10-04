# Optional generation adapters

This package implements Blockade's model and speech interfaces outside the
Blockade module. Import `cymonkey-content-plugin-endpoints/generation` from an
embedding host. It is independent of this module's content CLI executables.

```go
client, err := generation.New(generation.Config{
    Protocol: selectedProtocol,
    BaseURL: configuredAPIRoot,
    APIKey: configuredCredentialResolver,
    HTTPClient: hostHTTPClient,
})
if err != nil { return err }
response, err := client.Generate(ctx, blockade.GenerationRequest{
    Model: configuredModelID,
    Messages: []blockade.Message{blockade.TextMessage("user", prompt)},
})
```

API root and model are required, caller-supplied values. There are no built-in
provider profiles, endpoint defaults, model catalogs, or inferred credential
environment variables in this SDK. `EnvironmentKey(name)` is an opt-in resolver
for the exact variable named by the host. API dialect selection supplies only
wire encoding, paths, and headers, and works with compatible custom endpoints.

## Supported wire mappings

| Protocol | Operations and content |
| --- | --- |
| `OpenAI` (`openai-compatible`) | Chat Completions; text/image and WAV/MP3 input; function tools; JSON Schema output; SSE; transcription and speech |
| `Responses` (`responses-compatible`) | Responses; text/image/file input; function tools; JSON Schema output; SSE; opaque reasoning context round-trip |
| `Anthropic` (`anthropic`) | Messages; text/image/PDF input; tool use/results; JSON Schema output; SSE; signed/redacted thinking replay |
| `Gemini` (`gemini`) | Generate Content; inline/referenced text/image/audio/video/file; function tools/results; JSON Schema output; SSE; thoughts/signatures |
| `Cohere`, `Voyage` | Explicit embedding-only wire mappings; no generation inference |

An endpoint/model may implement fewer features than its dialect. This package
rejects unmapped modalities or output block types with
`blockade.ErrGenerationUnsupported`. It does not execute tools, enable hosted
built-in tools, select another endpoint, or retry errors. Structured output
schemas are forwarded and completed object output is locally validated. Tool
proposals and refusals remain distinct from structured object output.
Anthropic defaults an omitted output token limit to 4096 because the wire API
requires one. Speech defaults an omitted format to MP3; model and voice remain
explicit. Chat-compatible servers must support `max_completion_tokens` when
an output limit is supplied.

Responses requests use `store:false` and request encrypted reasoning context,
allowing callers to own the transcript. Reasoning context belongs to its
original adapter/model; do not forward it to a different dialect. Anthropic signed thinking and supported remote media references are mapped;
hosted uploads remain unsupported. Gemini emits function calls as whole native proposals, without enabling
incremental function-call-argument streaming.

`Stream` invokes callbacks synchronously. Callback errors stop the request;
incomplete streams fail rather than return partial tool arguments as success.
Events contain text/tool deltas and one validated completion snapshot. Response
bodies are bounded (32 MiB by default, configurable up to 256 MiB). HTTP errors
expose status, category, and numeric Retry-After without exposing response bodies
or credentials. There is no automatic retry. Supplied HTTP clients control their
own transport/redirect policy; the default client disables redirects.

`Synthesize` returns a bounded audio reader that the host must close. It never
plays audio. `Transcribe` takes explicit audio bytes and media type. See the
[`realtime`](../realtime/README.md) package for duplex model connections.

Wire references: [Chat Completions](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create),
[Anthropic streaming](https://platform.claude.com/docs/en/build-with-claude/streaming),
[Anthropic structured output](https://platform.claude.com/docs/en/build-with-claude/structured-outputs),
[Gemini Generate Content](https://ai.google.dev/api/generate-content).

## Embeddings and native options

`Client.Embed` supports explicit OpenAI-compatible, Cohere and Voyage embedding
protocols. Requests specify text or tokens and optional dimensions; unsupported
input kinds fail. Cohere requires the host's `EmbeddingInputType`. There is no
implicit truncation, normalization or endpoint/model selection. Bounded batch
embedding and vector helpers live in Blockade.

`GenerationRequest.Options` is an adapter-owned JSON object with a restricted
vocabulary. It cannot override endpoints, models, messages or tool definitions.
The package maps supported reasoning/thinking, sampling, tool-choice and
verbosity fields, rejecting unsupported keys and combinations. These fields
are model-dependent; the adapter never guesses capabilities from model names.

Transcription optionally requests metadata and word/segment timestamps, with
validated finite timing in the neutral response. Endpoint/model support is
required; the client does not retry with simpler options. See the official
[speech-to-text guide](https://developers.openai.com/api/docs/guides/speech-to-text).
