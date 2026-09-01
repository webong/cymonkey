# Cymonkey host

Cymonkey is the operator and host for standalone Jangolova and Blockade
processes. It does not embed either subsystem or replace their executables.

Validate and run a host manifest from the repository root:

```sh
mkdir -p .cache/bin
go build -o .cache/bin/jangolova ./cmd/jangolova
go build -o .cache/bin/blockade ./cmd/blockade
go build -o .cache/bin/cymonkey ./cmd/cymonkey

PATH="$PWD/.cache/bin:$PATH" \
  cymonkey validate --config infra/deploy/cymonkey/cymonkey.example.yaml

PATH="$PWD/.cache/bin:$PATH" \
  cymonkey run --config infra/deploy/cymonkey/cymonkey.example.yaml
```

The host starts components in manifest order, forwards their stdout and
stderr, and stops the remaining components when one exits or the host receives
SIGINT/SIGTERM. Component configuration is intentionally executable-oriented:
the host supplies commands, environment, and working directories but does not
know Blockade model internals or Jangolova target-driver internals.

Jangolova and Blockade remain independently usable:

```sh
jangolova serve-mcp
blockade serve --config infra/deploy/blockade/blockade.example.yaml
```

The host manifest is versioned as `cymonkey.config/v1alpha1`. Provider-specific
settings remain in the component's own configuration or environment.
