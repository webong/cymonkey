# Console configuration and serving

`pkg/console` provides composition APIs. `cymy console` is a host for explicitly
configured tools, models and existing Cymonkey resources. It makes one selected
invocation or model turn per request.

## Configuration

Use a JSON file with `apiVersion: "cymonkey.console/v1alpha1"`. The
[example](../pkg/console/examples/operator/config.json) uses placeholder model
and endpoint values that must be replaced with your chosen configuration.
Credentials are resolved only from environment variables named in that file.

`models` entries select an opaque catalog ID, provider ID, native model ID,
protocol, API root and optional native adapter `options`. Native options are
fixed by the host binding; tool callers cannot replace them.

`tools` entries contain a Console `definition` and one of these binding kinds:

| Kind | Required binding |
| --- | --- |
| `generate`, `embed` | `modelId` |
| `transcribe` | `modelId`, `maxAudioBytes` |
| `synthesize` | `modelId`, `voice`, `format`, `maxTextBytes`, `maxAudioBytes`, absolute existing `artifactDirectory`; optional fixed `instructions` |
| `display` | `instanceId`, `surfaceId`, `action`; optional host-selected `approvalId` |
| `observe`, `verify`, `wait` | `instanceId`, `surfaceId` |
| `see` | `instanceId`, app `surfaceId`, `viewerSurfaceId`, `maxCaptureBytes`, `maxCapturePixels` |
| `observed-action` | `instanceId`, `surfaceId`, `action`; optional host-selected `approvalId` |
| `device` | Existing `sessionId`, fixed capability in `action`, `resourceId` |

Display/device tools also require `operator.url` and an optional explicitly
named `operator.tokenEnvironment`. The host borrows existing instances and
granted sessions. Device binding is for JSON action/result operations; media
and large content use the existing device channel SDK.

Every tool definition must have a matching argument schema. Schemas are
checked before handlers run. The supported offline vocabulary includes local
references, compositions, property/item checks and bounds. Unsupported
assertion keywords fail registration; `format` is an annotation and `pattern`
uses Go regular expressions. Semantic resource checks remain in each handler.

An `mcp` entry specifies `connection.url`, optional `tokenEnvironment`, the
exact `protocolVersion`, a namespace `prefix`, and an explicit list of tool
names. Discovery performs no tool calls. Only selected names are registered.
SDK users can instead attach supplied stdio pipes; the CLI configuration
currently attaches HTTP servers only.

## CLI

```sh
cymy console models --config /absolute/path/console.json
cymy console tools --config /absolute/path/console.json
cymy console call --config /absolute/path/console.json --input /absolute/path/call.json
cymy console generate --config /absolute/path/console.json --input /absolute/path/request.json
cymy console stream --config /absolute/path/console.json --input /absolute/path/request.json
```

A call has `name`, object `arguments` and optional `id`. Generation uses
Blockade's `model` catalog ID and `messages`; configured tools are advertised
automatically. Proposed calls are returned as data. The CLI does not invoke
them or request another turn.

Synthesis writes a new private audio artifact to the configured directory and
returns its path, media type and byte count. Callers provide only text, and
cannot select another path, model or voice. Capture tools return finite PNG
bytes as base64 JSON; SDK hosts can implement a consumer/content store when
they want references instead.

## HTTP and MCP

```sh
cymy console serve --config /absolute/path/console.json \
  --bind 127.0.0.1:8788 --token-env MY_CONSOLE_TOKEN
cymy console mcp --config /absolute/path/console.json
```

HTTP serving requires an explicit loopback listener and bearer-token variable.
Browser origins must appear exactly in `allowedOrigins`. Each HTTP request has
a one-minute deadline. Configure `mcpProtocolVersion` to publish MCP tools;
stdio uses JSON-lines stdin/stdout and starts no child process.

| Route | Operation |
| --- | --- |
| `GET /v1/console/tools` | Tool definitions |
| `GET /v1/console/models` | Explicit model catalog |
| `POST /v1/console/calls` | One selected tool call |
| `POST /v1/console/generations` | One model turn |
| `POST /v1/console/streams` | One model turn as NDJSON events |
| `POST /mcp` | Stateless MCP tool server |

HTTP requests are bounded to 1 MiB. Handler errors are sanitized by default.
Library users embedding `httpapi.Server` or `mcp.Server` supply authentication,
origin policy, listeners and service lifetimes. The MCP server publishes tools;
the client additionally supports selected resource and prompt reads. There
are no background event subscriptions or sampling callbacks.

See the [extraction map](tachikoma-peekaboo-extraction.md) for ownership,
platform limits, remaining features and build evidence.
