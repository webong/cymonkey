# Cymonkey host and operator

Cymonkey exposes three public domains:

```text
Cymonkey host
  ├─ display     interaction, presentation, and direct tools
  ├─ content     observation and multimodal interpretation
  └─ device      input and output devices under explicit grants
```

The host owns process lifecycle, composition configuration, and operator-level
coordination. It does not absorb any library's implementation or make the
libraries depend on the host.

## Service boundaries

Display and content services can be deployed and upgraded independently:

```sh
cymy display serve-tools --bind 127.0.0.1:7393
cymy content serve --config infra/deploy/content/content.example.yaml \
  --bind 127.0.0.1:8091
```

The content example expects its separately packaged plugin and referenced
local model to be installed first. See
[`infra/deploy/content`](../infra/deploy/content/README.md).

For a coordinated observation, Cymonkey requests the ordinary,
policy-authorized `window.screenshot` display action, then supplies those
pixels to the content service. The two services communicate only through
Cymonkey's public contracts.

## Host boundary

The `cymy` executable reads `cymonkey.config/v1alpha1` and supervises the
commands listed in `components`:

```sh
cymy validate --config infra/deploy/cymonkey/cymonkey.example.yaml
cymy run --config infra/deploy/cymonkey/cymonkey.example.yaml
```

The host starts components in manifest order, forwards their standard output
and error streams, and stops the remaining components when one exits or when
the host receives SIGINT/SIGTERM. The manifest supplies executable commands,
environment, and optional working directories; the host does not interpret
model files, target protocols, or provider-specific settings.

Callers bind public contracts through the generic `services` map. The host
validates only the contract identifier, HTTP endpoint, and optional token
environment variable:

```yaml
services:
  display.interaction:
    endpoint: http://127.0.0.1:7391
    tokenEnvironment: CYMONKEY_DISPLAY_TOKEN
  content.interpretation:
    endpoint: http://127.0.0.1:8091
    tokenEnvironment: CYMONKEY_CONTENT_TOKEN
```

Library names do not appear in the internal host schema. The observation
composition layer selects the contracts it needs.

This is intentionally a first host slice. Future operator APIs can add
component health, discovery, restart policy, and coordinated routing without
changing the standalone subsystem contracts.

The first coordinator surface is a CLI command:

```sh
export CYMONKEY_DISPLAY_TOKEN="a-random-session-secret"
export CYMONKEY_CONTENT_TOKEN="another-random-session-secret"
cymy observe --config infra/deploy/cymonkey/cymonkey.example.yaml \
  --instance browser-1 --prompt "find the primary action"
```

The manifest passes credentials to the two Cymonkey services and keeps the
secret values out of the manifest.

See the runnable example in
[`infra/deploy/cymonkey`](../infra/deploy/cymonkey/README.md).
