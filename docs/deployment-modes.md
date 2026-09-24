# Deployment Modes

Jangolova's code is the same in every mode: it receives an already-running
target and attaches an interaction engine.

## Native host

Start Chromium using the native system's preferred mechanism and enable a
private CDP endpoint. Then attach Jangolova:

```sh
cymonkey provider connect-engine \
  --target-kind browser \
  --endpoint cdp=http://127.0.0.1:9222
```

The command defaults to automatic protocol-based selection. Use
`--adapter puppeteer` when a particular implementation is required.
Interrupting the command disconnects the adapter and leaves Chromium running.

For Firefox, start it with WebDriver BiDi enabled and give Puppeteer the direct
session endpoint:

```sh
cymonkey provider connect-engine \
  --adapter puppeteer \
  --target-kind browser \
  --endpoint webdriver-bidi=ws://127.0.0.1:9223/session
```

Safari uses WebDriver Classic. The target provider must start `safaridriver`,
create the browser session, and pass both its HTTP endpoint and session ID to
the provider API. Jangolova attaches to that session but never deletes it.

WebKitGTK and WPE WebKit use the same existing-session contract with the
`webkit-webdriver` adapter. The native host or Xallet starts
`WebKitWebDriver`/`WPEWebDriver`, creates the session, and supplies its ID.

Safari 27 beta and Safari Technology Preview can also use `safari-mcp`. Because
Apple exposes `safaridriver --mcp` over stdio, the target provider owns that
process and a private stdio-to-Streamable-HTTP MCP relay. Jangolova receives
only the relay endpoint.

## Standalone provider

```sh
export JANGOLOVA_PROVIDER_TOKEN="replace-with-a-random-secret"
cymonkey provider serve-engine-provider --bind 127.0.0.1:7391
```

Any authorized caller can submit target endpoints and use the semantic API.

## Independent container

The operator places Jangolova where it can reach the target endpoint:

```sh
docker run --rm \
  -e JANGOLOVA_PROVIDER_TOKEN \
  -p 127.0.0.1:7391:7391 \
  jangolova/engine-runtime:latest \
  serve-engine-provider --bind 0.0.0.0:7391
```

The image contains Jangolova, Node.js, Playwright Core, Puppeteer Core, the
browser interaction worker, and the dependency-free WebDriver Classic adapter.
It also contains the Safari MCP client. It deliberately contains no Chromium,
Firefox, WebKit runtime, Safari driver, or display server.

## Direct composite container

An operator may deliberately place Jangolova, a display server, a browser or
player, and an artifact server in one container. Co-location does not transfer
runtime ownership to Jangolova: the container supervisor starts and stops the
target components, then supplies their localhost endpoints to the interaction
provider.

```text
container supervisor
  +-- X11 or Wayland display
  +-- Chromium / Unity / Unreal target
  +-- local artifact server or mounted artifact directory
  +-- Jangolova engine provider
          |
          +-- attaches to caller-owned localhost endpoint
```

`presentation.mount` can select an `http`/`https` artifact location, or a
`target-file` location when that transport is explicitly enabled. Disconnect
still releases only the Jangolova connection. The direct-container smoke test
under `tests/docker` verifies artifact mounting and target preservation without
using Xallet.

## Xallet-managed

```text
Xallet provisions target
  Chromium / Unity / display / OCI placement
                 |
                 | typed endpoint or opaque handle
                 v
Jangolova interaction engine
  Playwright / Puppeteer / Unity bridge / Three.js
                 |
                 v
          semantic agent calls
```

Xallet owns target termination and endpoint publication. Jangolova owns only
its interaction connection and can be restarted or disconnected independently.

The same descriptor works when Jangolova and the target are on unrelated
remote machines. The caller supplies a URL reachable from Jangolova; no remote
or Xallet-specific execution mode is activated.
