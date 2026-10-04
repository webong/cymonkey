# Cymonkey host

Cymonkey is the operator and host for display, content, and device services.
Its public CLI is `cymy`.

Validate and run a host manifest from the repository root:

The example content component uses the configuration in
`scripts/deploy/content`. Install its plugin and local model first by following
the [content deployment guide](../content/README.md).

```sh
mkdir -p .cache/bin
go build -o .cache/bin/cymy ./src

PATH="$PWD/.cache/bin:$PATH" \
  cymy validate --config scripts/deploy/cymonkey/cymonkey.example.yaml

CYMONKEY_DISPLAY_TOKEN="a-random-session-secret" \
CYMONKEY_CONTENT_TOKEN="another-random-session-secret" \
PATH="$PWD/.cache/bin:$PATH" \
  cymy run --config scripts/deploy/cymonkey/cymonkey.example.yaml
```

The host starts components in manifest order, forwards their stdout and
stderr, and stops the remaining components when one exits or the host receives
SIGINT/SIGTERM. Component configuration is intentionally executable-oriented:
the host supplies commands, environment, and working directories while domain
adapters own model and target details.

The service interfaces remain independently usable through Cymonkey:

```sh
cymy display serve-tools
cymy content serve --config scripts/deploy/content/content.example.yaml
```

To coordinate an observation, use the same token and the interaction instance
identifier created through the display service:

```sh
CYMONKEY_DISPLAY_TOKEN="a-random-session-secret" \
PATH="$PWD/.cache/bin:$PATH" \
  cymy observe --config scripts/deploy/cymonkey/cymonkey.example.yaml \
  --instance browser-1 --prompt "find the primary action"
```

Cymonkey requests an authorized screenshot, then submits the pixels to the
content service. Neither service configures the other.

The host manifest is versioned as `cymonkey.config/v1alpha1`. Adapter-specific
settings remain in the component's own configuration or environment. The
current implementation uses private adapters behind these public boundaries.
