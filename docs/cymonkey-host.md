# Cymonkey host and operator

Cymonkey is the composition layer around two standalone libraries:

```text
Cymonkey host
  ├─ blockade     local and cloud inference interfaces (image now; sound planned)
  └─ jangolova    display interaction, presentation, and direct MCP tools
```

The host owns process lifecycle, composition configuration, and operator-level
coordination. It does not absorb either library's implementation or make the
libraries depend on the host.

## Standalone boundaries

Jangolova and Blockade can be built, deployed, upgraded, tested, and run
independently:

```sh
cymonkey provider serve-mcp --bind 127.0.0.1:7393
cymonkey blockade serve --config infra/deploy/blockade/blockade.example.yaml \
  --bind 127.0.0.1:8091
```

For a coordinated observation, Cymonkey requests Jangolova's ordinary,
policy-authorized `window.screenshot` interaction call, then supplies those
pixels to Blockade. Jangolova does not import, configure, or call Blockade.
Blockade does not know Cymonkey or Jangolova exists.

## Host boundary

The `cymonkey` executable reads `cymonkey.config/v1alpha1` and supervises the
commands listed in `components`:

```sh
cymonkey validate --config infra/deploy/cymonkey/cymonkey.example.yaml
cymonkey run --config infra/deploy/cymonkey/cymonkey.example.yaml
```

The host starts components in manifest order, forwards their standard output
and error streams, and stops the remaining components when one exits or when
the host receives SIGINT/SIGTERM. The manifest supplies executable commands,
environment, and optional working directories; the host does not interpret
model files, target protocols, or provider-specific settings.

This is intentionally a first host slice. Future operator APIs can add
component health, discovery, restart policy, and coordinated routing without
changing the standalone subsystem contracts.

The first coordinator surface is a CLI command:

```sh
export CYMONKEY_JANGOLOVA_TOKEN="a-random-session-secret"
cymonkey observe --config infra/deploy/cymonkey/cymonkey.example.yaml \
  --instance browser-1 --prompt "find the primary action"
```

The manifest passes `${CYMONKEY_JANGOLOVA_TOKEN}` to Jangolova and keeps the
same secret out of the manifest. A future Cymonkey API can expose this exact
workflow without changing either subsystem.

See the runnable example in
[`infra/deploy/cymonkey`](../infra/deploy/cymonkey/README.md).
