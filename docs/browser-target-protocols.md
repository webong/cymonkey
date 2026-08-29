# Browser Target Protocols and Xallet Handoff

This document defines how a target provider such as Xallet exposes browsers to
Jangolova. It is the implementation reference for the corresponding Xallet
work.

## Non-negotiable ownership rule

Jangolova owns the interaction library and its adapter-local worker. Xallet, a
native host, or another target provider owns the browser, browser driver,
display, network placement, target session, credentials, and termination.

An endpoint always flows into Jangolova. Disconnecting or deleting a Jangolova
interaction instance releases only the Jangolova connection. It must not stop
the browser, close its display, or delete a caller-owned WebDriver session.

```text
Agent
  |
  | hello / capabilities / describe / act / events
  v
Jangolova interaction provider
  |
  | caller-supplied typed endpoint and optional opaque handle
  v
Browser or browser-driver session
  ^
  |
Xallet or native target provider owns creation, policy, and destruction
```

## Supported target contracts

| Browser path | Jangolova adapter | Endpoint protocol | Target-provider input |
|---|---|---|---|
| Chromium-compatible browser | `playwright` | `cdp` | HTTP(S) or WS(S) CDP endpoint |
| Chromium-compatible browser | `puppeteer` | `cdp` | HTTP(S) or WS(S) CDP endpoint |
| Firefox or BiDi-capable Chromium | `puppeteer` | `webdriver-bidi` | Direct WS(S) BiDi session endpoint |
| Safari or W3C-compatible driver | `webdriver-classic` | `webdriver` | HTTP(S) driver endpoint plus an existing session ID |
| WebKitGTK, WPE WebKit, or Safari | `webkit-webdriver` | `webdriver` | HTTP(S) driver endpoint plus an existing session ID |
| Safari 27 beta or Safari Technology Preview | `safari-mcp` | `mcp-streamable-http` | HTTP(S) MCP relay endpoint owned by the target provider |

Playwright currently accepts CDP only in Jangolova. Puppeteer accepts CDP and
WebDriver BiDi. The WebDriver Classic adapter is a small protocol client and
does not require Playwright, Puppeteer, or a browser binary. The named
`webkit-webdriver` adapter uses that same target-preserving implementation but
advertises WebKit-specific discovery metadata.

### Chromium over CDP

```json
{
  "apiVersion": "interaction.engine/v1alpha1",
  "instanceId": "chromium-one",
  "engine": {"adapter": "playwright"},
  "target": {
    "kind": "browser",
    "endpoints": [{
      "name": "cdp",
      "protocol": "cdp",
      "url": "http://browser.internal:9222"
    }]
  }
}
```

Use `puppeteer` instead of `playwright` when desired. The target provider starts
Chromium with a private debugging endpoint and controls when Chromium stops.

### Firefox or Chromium over WebDriver BiDi

```json
{
  "apiVersion": "interaction.engine/v1alpha1",
  "instanceId": "firefox-one",
  "engine": {"adapter": "puppeteer"},
  "target": {
    "kind": "browser",
    "endpoints": [{
      "name": "webdriver-bidi",
      "protocol": "webdriver-bidi",
      "url": "ws://firefox.internal:9223/session"
    }]
  }
}
```

The URL must be a direct `ws://` or `wss://` BiDi endpoint. Jangolova passes it
to Puppeteer as an attachment target and does not launch Firefox. The Docker
fixture verifies this path against externally started Firefox and confirms the
browser survives Jangolova disconnect.

### Safari over an existing WebDriver Classic session

```json
{
  "apiVersion": "interaction.engine/v1alpha1",
  "instanceId": "safari-one",
  "engine": {"adapter": "webdriver-classic"},
  "target": {
    "kind": "browser",
    "endpoints": [{
      "name": "webdriver",
      "protocol": "webdriver",
      "url": "http://mac-runner.internal:4444"
    }],
    "handles": {
      "webdriver.sessionId": "xallet-created-session-id"
    }
  }
}
```

The target provider must start and configure `safaridriver`, create the session,
and retain its session ID before asking Jangolova to attach. Jangolova verifies
the session with `GET /session/{sessionId}/url`, uses only commands scoped to
that ID, and deliberately never sends `DELETE /session/{sessionId}`.

### WebKitGTK or WPE WebKit over WebDriver

Use the same endpoint and handle shape as Safari, but select
`webkit-webdriver`:

```json
{
  "apiVersion": "interaction.engine/v1alpha1",
  "instanceId": "webkit-one",
  "engine": {"adapter": "webkit-webdriver"},
  "target": {
    "kind": "browser",
    "endpoints": [{
      "name": "webdriver",
      "protocol": "webdriver",
      "url": "http://webkit.internal:4445"
    }],
    "handles": {
      "webdriver.sessionId": "xallet-created-webkit-session"
    }
  }
}
```

The target provider runs `WebKitWebDriver` for WebKitGTK or `WPEWebDriver` for
WPE WebKit, creates the browser session, and owns its display and cleanup.
Jangolova's test fixture proves navigation and JavaScript evaluation against an
external WebKitGTK MiniBrowser session and confirms that session survives
Jangolova disconnect.

### Safari over MCP

Apple currently starts its Safari MCP server with `safaridriver --mcp` over
stdio. Jangolova must not launch that process. Xallet or the native target
provider owns `safaridriver`, Safari, and a private standard MCP relay that
converts the stdio transport to Streamable HTTP.

```json
{
  "apiVersion": "interaction.engine/v1alpha1",
  "instanceId": "safari-mcp-one",
  "engine": {
    "adapter": "safari-mcp",
    "options": {
      "protocolVersion": "2025-06-18",
      "bearerTokenEnv": "SAFARI_MCP_RELAY_TOKEN"
    }
  },
  "target": {
    "kind": "browser",
    "endpoints": [{
      "name": "safari-mcp",
      "protocol": "mcp-streamable-http",
      "url": "http://safari-mcp-relay.internal/mcp"
    }]
  }
}
```

At connection time Jangolova initializes MCP, discovers `tools/list`, and
publishes every returned tool as `mcp.tool.<tool-name>` with the server's input
schema. It additionally maps these stable Safari tools when present:

- `navigate_to_url` to `window.navigate`;
- `evaluate_javascript` to `window.evaluate`;
- `screenshot` to `window.screenshot`;
- `page_interactions` to schema-preserving `browser.interact`;
- `page_info` into `describe`.

Agents can call any discovered tool through `mcp.call` or its generated
`mcp.tool.*` capability, including console, DOM, network, tab, viewport, and
accessibility-oriented operations. Disconnect closes only Jangolova's HTTP
connection; it sends no HTTP `DELETE` and does not stop the MCP server.

## Xallet implementation checklist

Xallet should implement a browser target provider that:

1. Selects and launches the requested browser or browser driver in an OCI
   workload, VM, physical host, or native user session.
2. Creates any required display, profile, volume, and operating-system session.
3. Publishes a private CDP, WebDriver BiDi, WebDriver, or MCP relay endpoint
   reachable from the selected Jangolova placement.
4. Creates the WebDriver Classic session when that protocol is selected and
   supplies `webdriver.sessionId` as an opaque handle.
5. For Safari MCP, starts `safaridriver --mcp`, owns the resulting Safari
   automation session, and exposes it through an authenticated private
   stdio-to-Streamable-HTTP MCP relay.
6. Translates container or host-local addresses into the URL that is actually
   reachable from Jangolova. `127.0.0.1` is valid only when both processes share
   the same network namespace.
7. Protects the endpoint through private networking, authorization gateways,
   and policy. Credentials must not be embedded in endpoint URLs.
8. Sends the target contract to `POST /v1/instances` and records the returned
   Jangolova interaction instance ID separately from its own target/session ID.
9. On shutdown, deletes or disconnects the Jangolova interaction instance
   first, then closes the driver session, browser, display, and workload.
10. Owns reconciliation after crashes: an orphaned browser, MCP server, relay,
    or driver session is a Xallet resource, not a Jangolova resource.

The provider should select protocols from declared Jangolova engine
capabilities (`target.cdp`, `target.webdriver-bidi`, `target.webdriver`,
`target.webkit.webdriver`, or `target.safari-mcp`), not from hard-coded browser
ports in Xallet core. Browser templates may define
defaults such as port 9222, but the resolved endpoint belongs in target data.

### Xallet acceptance criteria

For every supported browser template, an integration test should prove that:

- Xallet creates the browser, display, and driver resources without asking
  Jangolova to provision them.
- The resolved typed endpoint is reachable from the chosen Jangolova placement.
- Xallet can create a Jangolova interaction instance and call `hello`,
  `capabilities`, `describe`, `act`, and `events`.
- Deleting the Jangolova instance leaves the browser and any caller-owned
  WebDriver session alive.
- Deleting the Xallet target subsequently removes the driver session, browser,
  display, networking, and OCI/native resources.
- Xallet core has no permanent dependency on a browser-specific default port;
  defaults live in replaceable browser templates or provider capabilities.
- Safari MCP tests prove the relay and `safaridriver` survive Jangolova
  disconnect and are terminated only by Xallet cleanup.

## Lifecycle sequence

```text
Xallet/native              Jangolova                    Browser/driver
     |                         |                              |
     | create target/session  |                              |
     |------------------------------------------------------->|
     |                         |                              |
     | POST /v1/instances     |                              |
     | endpoint + handle      |                              |
     |------------------------>| attach only                  |
     |                         |----------------------------->|
     |                         |                              |
     | semantic calls         | protocol commands            |
     |------------------------>|----------------------------->|
     |                         |                              |
     | DELETE interaction     | disconnect only              |
     |------------------------>|                              |
     |                         |                              |
     | delete session/target  |                              |
     |------------------------------------------------------->|
```

## Security and failure behavior

- Bind raw browser-control endpoints to a private interface or isolated
  application network. They grant powerful control over the target.
- Give Jangolova a reachable endpoint, not necessarily the endpoint originally
  advertised inside a container.
- A failed Jangolova worker may make the interaction unhealthy, but it does not
  imply the target should be killed. Xallet decides whether to reconnect,
  replace, or preserve the target.
- A failed target should surface as unhealthy interaction health. Xallet remains
  responsible for target diagnostics and replacement.
- Rotate the Jangolova provider bearer token independently of browser-control
  endpoints and target credentials.

## Experimental availability

The Jangolova Safari MCP adapter is implemented, but Apple's server is currently
associated with Safari 27 beta and Safari Technology Preview 247 rather than a
stable Safari baseline. Xallet should therefore advertise the MCP target only
after detecting `safaridriver --mcp`; otherwise it should use the existing
WebDriver session contract.

Firefox Marionette and WebKit's Remote Inspector remain browser-internal or
implementation-specific control channels. They should not become primary
public Jangolova target contracts while WebDriver BiDi and W3C WebDriver cover
the required portable paths. They can be considered later as explicitly
experimental adapters for capabilities unavailable through standard protocols.

## Protocol references

- [W3C WebDriver BiDi](https://www.w3.org/TR/webdriver-bidi/)
- [Firefox direct BiDi connection](https://developer.mozilla.org/en-US/docs/Web/WebDriver/How_to/Create_BiDi_connection)
- [Puppeteer WebDriver BiDi support](https://pptr.dev/next/webdriver-bidi)
- [Apple: Testing with WebDriver in Safari](https://developer.apple.com/documentation/webkit/testing-with-webdriver-in-safari)
- [WebKitGTK automation context API](https://webkitgtk.org/reference/webkit2gtk/2.41.1/method.WebContext.set_automation_allowed.html)
- [WebKit: Safari MCP server introduction](https://webkit.org/blog/18136/introducing-the-safari-mcp-server-for-web-developers/)
- [MCP Streamable HTTP transport](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports)
