# Experience Bridge Protocol

> This is the generic/browser bridge and the earlier native experiment. New
> Unity and Unreal semantic presentation integrations use Cymonkey's `render`
> domain, including explicit resource registration and a caller-owned WebSocket
> endpoint.

The Jangolova experience bridge lets a cooperative interactive runtime expose
semantic capabilities without coupling target lifecycle to Three.js,
Unity, Unreal, or a particular caller transport.

The current protocol identifier is:

```text
jangolova.bridge/v1alpha1
```

Its Go wire types and method constants live in `internal/bridge`. The protocol
has five operations:

| Operation | Purpose |
| --- | --- |
| `hello` | Negotiate the protocol version and identify the implementation. |
| `capabilities` | Advertise bounded actions and their JSON input schemas. |
| `describe` | Return current serializable engine or scene state. |
| `act` | Execute one advertised action. |
| `events` | Read typed events after an opaque cursor. |

## Handshake

```json
{
  "protocolVersion": "jangolova.bridge/v1alpha1",
  "implementation": {
    "name": "jangolova-threejs-scene",
    "version": "185"
  },
  "features": ["events.cursor"]
}
```

An adapter must reject an incompatible protocol version instead of guessing at
wire compatibility.

## Events

Event reads are non-destructive and cursor-addressed:

```json
{
  "after": "12",
  "types": ["pointer.select"],
  "limit": 100
}
```

```json
{
  "events": [
    {
      "id": "13",
      "type": "pointer.select",
      "occurredAt": "2026-07-29T12:00:00Z",
      "data": {"objectId": "agent-cube", "x": 640, "y": 360}
    }
  ],
  "cursor": "13"
}
```

Event IDs are ordered within one bridge instance. Cursors are opaque to
Jangolova clients and are supplied unchanged on the next read. A bridge should
retain a bounded event history and document how it handles a cursor older than
that history.

Implementations may support immediate reads or bounded long polling according
to their advertised features.

## Transport mappings

The browser transport exposes the five operations on `window.jangolova`.
Jangolova attaches Playwright or Puppeteer to a caller-owned CDP/BiDi endpoint,
or WebDriver Classic to a caller-owned existing driver session, and
invokes the page bridge without owning the browser lifecycle.
Safari MCP tools can also be transported through a caller-owned MCP relay and
are discovered dynamically before being mapped to bridge capabilities.

The native transport uses an authenticated loopback WebSocket. Jangolova sends:

```json
{"id":1,"method":"describe","params":{}}
```

The engine replies with either a result:

```json
{"id":1,"result":{"scene":"ready"}}
```

or a structured error:

```json
{"id":1,"error":{"code":"invalid_action","message":"Object was not found"}}
```

The target owner starts the native process and injects the endpoint, bearer
token, and protocol identifier supplied for the Jangolova interaction session.
Transport framing and semantic calls belong to Jangolova; target process and
display ownership remain external.

## Unity

The installable Unity package lives at
`integrations/unity/com.jangolova.bridge`. It uses `ClientWebSocket` to connect
outward to the native bridge and dispatches protocol calls onto Unity's main
thread before touching scene objects.

Its built-in scene bridge exposes primitive object lifecycle, transform,
camera, state-description, and cursor-event capabilities. Projects can replace
the built-in scene handler with a domain-specific `IJangolovaBridgeHandler`;
the transport contract does not require a universal scene graph.

The protocol deliberately does not define a universal scene graph. A Unity
project may expose GameObject and component actions while an Unreal project
exposes Actor and Level actions. Common conventions should be introduced only
where multiple engines prove they are useful.

## Conformance

Jangolova's reusable conformance validator checks the handshake, implementation
identity, features, capabilities, description, cursor event behavior, and an
optional explicitly selected read-only action probe. Browser and native
integrations can run the same validator without importing display-session
policy.

## Trust

Bridge capabilities and effect declarations are untrusted descriptions. Xallet
or another caller combines them with its own trust and authorization policy
before invoking an action.
