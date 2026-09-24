# Caller-supplied interaction targets

Jangolova is location-agnostic in both directions. Its own process may run on
a native machine, in a container, in a VM, or remotely. The target may be in
any of those places independently. The interaction contract contains no local
or remote mode.

The caller points Jangolova at an already-running target using the
[provider-neutral target schema](../src/protocol/target/v1/target.schema.json).
The caller may be a person, an agent, a native launcher, a container
supervisor, a VM manager, Xallet, or another orchestration system.

```json
{
  "apiVersion": "interaction.target/v1alpha1",
  "targetId": "desktop-browser-42",
  "kind": "browser",
  "endpoints": [{
    "name": "browser-control",
    "protocol": "cdp",
    "url": "wss://browser.example/control/42",
    "credentialRef": "browser-session-42",
    "tlsRef": "browser-cluster-ca",
    "audience": "engine",
    "metadata": {"network.scope": "private"}
  }],
  "metadata": {"owner.kind": "external"}
}
```

`credentialRef` and `tlsRef` are opaque references, never inline secrets.
Jangolova resolves them through its deployment-neutral
[target connection security layer](target-connection-security.md) immediately
before connecting the selected adapter. The caller controls the resolver and
the underlying secret storage.

The endpoint URL must be reachable from Jangolova's network namespace. Address
translation, tunnels, firewall rules, service discovery, and runtime lifecycle
remain caller responsibilities. Unity and Unreal semantic targets advertise a
`websocket` endpoint; its URL is the target-owned authenticated WebSocket
for a Cymonkey render runtime, not a display or pixel stream.
`127.0.0.1`, container DNS, a VM address, and
a remote TLS URL are treated identically after protocol validation.

## Automatic engine selection

Set `engine.adapter` to `auto` and optionally require capabilities:

```json
{
  "apiVersion": "interaction.engine/v1alpha1",
  "instanceId": "interaction-42",
  "engine": {
    "adapter": "auto",
    "requiredCapabilities": ["presentation.mount"]
  },
  "target": {
    "apiVersion": "interaction.target/v1alpha1",
    "targetId": "desktop-browser-42",
    "kind": "browser",
    "endpoints": [{
      "name": "browser-control",
      "protocol": "cdp",
      "url": "wss://browser.example/control/42"
    }]
  }
}
```

Selection is deterministic and considers only available engine capabilities,
offered protocols, and caller-required capabilities. It never examines the
hostname or deployment owner. Current protocol mappings are:

- CDP → Playwright, Puppeteer, or web presentation according to required
  capabilities;
- WebDriver BiDi → Puppeteer;
- WebDriver Classic → generic or WebKit WebDriver;
- MCP Streamable HTTP → Safari MCP.
- Cymonkey WebSocket → Cymonkey render for Unity, Unreal, or another conforming fixture.

An explicit adapter name remains supported when the caller needs a specific
implementation. The standalone `connect-engine` command defaults to `auto` and
accepts repeated `--require-capability` flags.

## Ownership invariant

Connecting transfers no lifecycle ownership. Jangolova may close its CDP,
WebDriver, MCP, or cooperative-bridge connection, but it must not stop the
browser, application, VM, container, display, relay, or target session.
