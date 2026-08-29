# Jangolova browser-extension control plane

The private browser-extension protocol is
`jangolova.browser-extension/v1alpha1`. It carries Jangolova platform calls,
Cymonkey's five semantic operations, including render-domain calls, over authenticated
extension-origin, Xallet Spook, or optional outbound WebSocket transports.
Website code cannot access this plane.

## Authorization policy

Transport authentication establishes caller identity; it does not authorize an
action. Every private call is resolved to a context containing:

- caller: `xallet-spook`, `authenticated-websocket`, or `extension-origin`;
- semantic capability and effect (`read`, `write`, or `external`);
- resolved target tab and HTTP(S) origin when the action targets a page;
- augmentation ID when one is present.

The stored version 1 policy has a default decision and bounded rules. Rules can
match any combination of caller, capability, effect, origin, tab ID, and
augmentation ID. `*` wildcards are supported in capability, origin, and
augmentation patterns. Matching deny rules take precedence over matching allow
rules.

Without a configured policy, the extension uses a default-deny policy. Read
effects are allowed. Write/external effects are denied. An authenticated Xallet
Spook or extension-origin caller may bootstrap `policy.replace` and
`control.websocket.*`; an outbound WebSocket cannot grant itself authority.

Example replacement call:

```json
{
  "type": "JANGOLOVA_EXTENSION_CALL",
  "method": "policy.replace",
  "params": {
    "policy": {
      "version": 1,
      "defaultDecision": "deny",
      "rules": [{
        "id": "allow-wikipedia-overlays",
        "decision": "allow",
        "callers": ["authenticated-websocket"],
        "capabilities": ["overlay.*"],
        "effects": ["write"],
        "origins": ["https://*.wikipedia.org"]
      }]
    }
  }
}
```

`policy.describe` returns the effective policy and whether it is configured or
the built-in default. Policy documents contain no credentials.

## Audit events

The shared extension event log records:

- `audit.control.requested`
- `audit.control.succeeded`
- `audit.control.denied`
- `audit.control.failed`

Audit data contains only caller, capability, effect, policy mode/rule,
augmentation ID, and resolved tab/origin. Parameters, userscript source,
credentials, request bodies, page contents, and WebSocket tokens are omitted.

## Optional outbound WebSocket

The same extension artifact can initiate an authenticated WebSocket when a
trusted bootstrap caller invokes `control.websocket.configure` with a
caller-owned endpoint, token, and expiry. This is optional and has no Xallet
dependency.

- Remote endpoints require `wss:`; `ws:` is accepted only on loopback.
- Tokens must expire within 24 hours and are held in extension session storage.
- The extension sends `JANGOLOVA_EXTENSION_AUTH` after opening the socket.
- It accepts control calls only after
  `JANGOLOVA_EXTENSION_AUTHENTICATED` is received.
- Heartbeats run every 20 seconds and reconnect delay is bounded at 30 seconds.
- `describe` returns endpoint/status/expiry but never the token.
- `control.websocket.disable` removes configuration and closes the socket.

Xallet Spook messaging, extension-origin/CDP control, standalone page-safe
operation, and this WebSocket remain runtime choices in one build.

## Generated bindings

The canonical schema is
`protocol/browser-extension/v1alpha1/protocol.schema.json`. Generate the
checked-in TypeScript and Go bindings with:

```sh
npm run generate:browser-extension-protocol
npm run check:browser-extension-protocol
```

Recorded exchanges under `protocol/browser-extension/v1alpha1/fixtures`
verify the one `JANGOLOVA_EXTENSION_CALL` envelope, including its nested
`cymonkey.call` subsystem method. Generated files include the source schema
digest and must not be edited manually.
