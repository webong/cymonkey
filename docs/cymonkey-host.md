# Cymonkey host and operator

Cymonkey is the composition layer around two standalone subsystems:

```text
Cymonkey host
  ├─ blockade     visual observation and inference
  └─ jangolova    interaction, presentation, and direct MCP tools
```

The host owns process lifecycle, composition configuration, and operator-level
coordination. It does not absorb either subsystem's implementation or make
their standalone commands depend on the host.

## Standalone boundaries

Jangolova and Blockade can be built, deployed, upgraded, tested, and run
independently:

```sh
jangolova serve-mcp --bind 127.0.0.1:7393
blockade serve --config infra/deploy/blockade/blockade.example.yaml \
  --bind 127.0.0.1:8091
```

Jangolova can use the separately running Blockade service through its existing
`JANGOLOVA_BLOCKADE_ENDPOINT` setting. Blockade does not need to know that
Cymonkey or Jangolova exists.

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

See the runnable example in
[`infra/deploy/cymonkey`](../infra/deploy/cymonkey/README.md).
