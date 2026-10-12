---
name: provider-adapter
description: Create or update Cymonkey content plugins that map local or hosted model responses into Cymonkey observations and interpretations. Use for provider APIs, executable plugins, or source packages; do not expose the internal content library contract.
---

# Cymonkey content plugin

Cymonkey owns the public plugin, configuration, model, runtime, CLI, and HTTP
contracts. A plugin supplies an executable or a verified source package and
implements `cymonkey.content@v1alpha1`. Internal library types stay behind
Cymonkey's composition bridge.

## Before implementation

1. Inspect repository instructions, the current diff, and existing content
   plugins. Preserve unrelated work.
2. Read [the plugin contract](references/adapter-contract.md).
3. For a hosted API, obtain its current official documentation and identify
   input formats, structured output, authentication, limits, timeout behavior,
   and errors.
4. Read [the mapping rules](references/provider-mapping.md) before translating
   provider-native output.

## Choose the package form

- Use an executable `package.json` (ext.package/v1) package when the plugin owns its runtime.
- Use a verified source package with `content.json` when Cymonkey should launch
  it with an installed managed Python runtime. The package supplies source and
  dependencies; it does not select an ambient Python executable.
- Keep provider SDKs, endpoints, model names, retry rules, and response shapes
  inside the plugin package.

## Implementation requirements

- Declare `cymonkey.content@v1alpha1` in `package.json`.
- Implement methods under `cymonkey.content.*` and use Cymonkey API versions in
  every public request and response.
- Preserve request IDs and original media bytes unless the upstream service
  requires a documented conversion.
- Propagate cancellation and apply a bounded timeout. Retry only safe requests
  after transient failures and honor rate limit guidance.
- Put secret environment references in `adapters[].secrets`. Never place secret
  values in settings, manifests, fixtures, logs, evidence, or returned errors.
- Treat malformed or incomplete model output as a typed plugin error. Do not
  fabricate observations or findings.
- Keep authentication, rate limit, timeout, cancellation, unavailable, invalid
  request, and invalid response failures distinguishable.

## Verification

Add redacted response fixtures and focused tests for request serialization,
authentication without disclosure, successful mapping, malformed output,
out-of-range values, cancellation, timeouts, rate limits, and non-success HTTP
responses. Validate final values against Cymonkey's public content contract.

Use a local fake server or in-memory client in ordinary tests. Make a paid or
credentialed live call only when the user explicitly requests it. Update the
public plugin example and content documentation when registering the plugin.
