# Cymonkey host

Cymonkey is the operator and host for the standalone Jangolova and Blockade
libraries. It exposes their interfaces through one `cymonkey` executable.

Validate and run a host manifest from the repository root:

```sh
mkdir -p .cache/bin
go build -o .cache/bin/cymonkey ./src

PATH="$PWD/.cache/bin:$PATH" \
  cymonkey validate --config infra/deploy/cymonkey/cymonkey.example.yaml

CYMONKEY_JANGOLOVA_TOKEN="a-random-session-secret" \
PATH="$PWD/.cache/bin:$PATH" \
  cymonkey run --config infra/deploy/cymonkey/cymonkey.example.yaml
```

The host starts components in manifest order, forwards their stdout and
stderr, and stops the remaining components when one exits or the host receives
SIGINT/SIGTERM. Component configuration is intentionally executable-oriented:
the host supplies commands, environment, and working directories but does not
know Blockade model internals or Jangolova target-driver internals.

The library interfaces remain independently usable through Cymonkey:

```sh
cymonkey provider serve-mcp
cymonkey blockade serve --config infra/deploy/blockade/blockade.example.yaml
```

To coordinate an observation, use the same token and the interaction instance
identifier created through Jangolova:

```sh
CYMONKEY_JANGOLOVA_TOKEN="a-random-session-secret" \
PATH="$PWD/.cache/bin:$PATH" \
  cymonkey observe --config infra/deploy/cymonkey/cymonkey.example.yaml \
  --instance browser-1 --prompt "find the primary action"
```

Cymonkey calls Jangolova only for an authorized screenshot, then submits the
pixels to Blockade. Neither standalone subsystem imports or configures the
other.

The host manifest is versioned as `cymonkey.config/v1alpha1`. Provider-specific
settings remain in the component's own configuration or environment.
