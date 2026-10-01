# Console

Console is a standalone interface library for composing callable tools from
other SDKs and application code. It lets a host publish tool definitions,
show them to a person or model, and explicitly dispatch a selected call.

Console is a tool system for humans and agents. A host explicitly requests a
model turn or invokes a registered tool. Console does not choose tools, run an
autonomous loop, persist conversations, retry calls, or decide whether a call
is approved. The host owns those choices.

## Core API

Register tools in a zero-value registry, expose their definitions to a caller,
and invoke only the call selected by that caller:

```go
var tools console.Registry

lookup, err := console.NewJSONTool[struct {
	Query string `json:"query"`
}, struct {
	Matches []string `json:"matches"`
}](console.Definition{
	Name:        "search_notes",
	Description: "Search the caller's approved notes collection.",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`),
}, func(ctx context.Context, input struct {
	Query string `json:"query"`
}) (struct {
	Matches []string `json:"matches"`
}, error) {
	return searchApprovedNotes(ctx, input.Query)
})
if err != nil {
	return err
}
if err := tools.Register(lookup); err != nil {
	return err
}

definitions := tools.Definitions() // adapt to a model API or render in a UI
_ = definitions

result, err := tools.Invoke(ctx, console.Call{
	Name:      "search_notes",
	Arguments: json.RawMessage(`{"query":"meeting notes"}`),
})
```

`NewTool` accepts a raw JSON handler when typed input and output structs are
not convenient. Tool outputs must be valid JSON. The registry checks that
arguments are JSON objects and, for `NewJSONTool`, rejects unknown fields and
trailing JSON values.
Console publishes the schema but does not evaluate its constraints; handlers
must enforce required fields and application-specific rules.

## Composing Cymonkey SDKs

A host can adapt Jangolova actions, Board device capabilities, Blockade
operations, and application functions into Console tools. The model-facing
tool schema is separate from the implementation and can be translated to
whatever format a provider expects.

For example, a host could expose a Jangolova page query as a read tool and a
Board keyboard action as a state-changing tool. Console only invokes a tool
after the host chooses that call. The host remains responsible for approval,
policy, and any additional limits required by its environment.

The root package uses Blockade's neutral model contracts. `console/bindings`
adds Jangolova, Board, and content-engine bindings. `console/mcp` attaches tools
from a caller-selected server. Console has no dependency on a provider client,
the Cymonkey executable, or a UI framework.

## One explicit model turn

`ModelTools` advertises a registry to any caller-supplied `blockade.Generator`.
`Generate` makes exactly one model request. `Stream` requires the model to
implement `blockade.StreamingGenerator`; it never simulates streaming.

```go
composition := console.ModelTools{Model: selectedGenerator, Tools: &tools}
messages := []blockade.Message{blockade.TextMessage("user", prompt)}
response, err := composition.Generate(ctx, blockade.GenerationRequest{
    Model: selectedModelID,
    Messages: messages,
})
if err != nil { return err }
proposals, err := console.ProposedCalls(response)
if err != nil { return err }
// Present proposals in a UI or pass them to the host's decision policy.
```

The host can invoke an approved proposal with `tools.Invoke(ctx, proposal)`.
After every proposal is resolved, `console.Continue(messages, response,
results)` returns a copied transcript, validating result IDs and tool names.
It does not request another turn. Use `console.FailureResult` to supply a
host-chosen public error for a rejected or failed proposal. The host explicitly
decides whether to submit the updated transcript to the model.

## SDK bindings

- `bindings.Display` binds one fixed Jangolova action to an existing caller.
  Its caller must enforce the host's approval/policy decisions.
- `bindings.Device` binds a fixed Board capability and resource ID to an
  existing granted connection. Arguments cannot replace that binding.
- `bindings.Interpret` wraps a selected Blockade content engine and validates
  its request and response.
- `bindings.Generate` exposes one fixed model as a tool, returning a validated
  generation response. Its input cannot select a model or supply nested tools.
- `bindings.Transcribe` exposes a fixed transcription model with an explicit
  audio byte ceiling. Audio bytes use JSON's base64 representation.
- `bindings.Synthesize` fixes model, voice, format, instructions, and text byte
  ceiling. The host supplies a synchronous audio consumer that returns JSON
  metadata. The binding closes the audio response after the consumer returns,
  including on failure; it never chooses a file or device destination.

Bindings borrow connections. The host creates them, selects their scope, and
closes them. Application-specific tools use `NewTool` or `NewJSONTool`.

`Generate` arguments use `bindings.GenerationInput`; transcription uses
`bindings.TranscriptionInput`; synthesis uses `bindings.SpeechInput`. Define a
matching schema for each bound tool. Typed decoding rejects unknown fields,
so arguments such as `model`, `voice`, or an output path cannot replace the
fixed host binding. Binding validation and handler checks supplement the
advertised schema; Console does not evaluate JSON Schema.

An audio consumer can store a bounded response in an existing host content
store, or write to an already approved playback channel. It must enforce its
own byte/time limits and consume the stream before returning. The result can
be a JSON content reference or receipt; large audio bytes remain outside the
tool-result JSON. Destination selection and authorization stay in the host.

## Runnable example

From this directory:

```sh
go run ./examples/handoff                         # inspect one proposal
go run ./examples/handoff -action invoke -key board
go run ./examples/handoff -action reject          # supply a public failure
go run ./examples/handoff -action direct -key jangolova
```

The [handoff example](examples/handoff/README.md) uses an in-memory note tool
and a local scripted generator. It needs no model credentials or device
attachments. It shows model/tool call counts, and stops after preparing a
continuation transcript. Replace the scripted generator with your configured
model client in an embedding application.

## MCP attachment

`mcp.NewHTTP` uses an explicitly configured Streamable HTTP endpoint;
`mcp.NewStdio` attaches to supplied process pipes. Neither starts a server or
process. Initialize with the protocol version your host supports, then call
`Client.Tools(ctx, prefix)` to get a bounded, paginated catalog snapshot.
Select tools and register them yourself; `tools/call` occurs only on invocation.

MCP content and `isError` results are preserved as JSON for the host to consume.
Server notifications and requests such as sampling are not executed. HTTP
supports JSON and SSE POST responses, session IDs, and explicit session release;
background GET event subscriptions and reconnection are outside this client.
Stdio cancellation closes the borrowed pipe attachment to unblock I/O. The
host retains process ownership.

See [SDK model and tool composition](../../docs/sdk-model-tools.md) for the
feature mapping and module ownership.
