# Jangolova module registry

The Jangolova registry is the extension catalog for render and runtime
modules. It is owned by Jangolova; Cymonkey is its discovery, policy, cache,
and mount client.

## Lifecycle

```text
discover metadata → filter by policy/platform/protocol → approve
    → pull immutable artifact → verify SHA-256 → mount into caller target
```

Discovery does not execute code. A registry entry may be metadata-only while a
module is being built; it becomes pullable when it contains a platform artifact
with a HTTPS URL and SHA-256 digest.

The reference snapshot lives at
[`lib/jangolova/registry/index.json`](../lib/jangolova/registry/index.json). It is a
development fixture, not a replacement for a signed production registry.

The reusable Jangolova registry package is `lib/jangolova/registry`. Its
`Discover` function accepts HTTPS endpoints (or loopback HTTP for local
fixtures), `Select` filters an advertised module by platform, and `Pull` writes
a temporary cache file, checks its digest, and atomically publishes it.
Pulling never launches a process, loads a shared library, or mounts a target;
those operations remain explicit provider actions after approval. Cymonkey
imports this public package as its coordinator-side client.

For a simple operator-facing flow, the `cymonkey modules` interface exposes the
same stages:

```sh
cymonkey modules discover --registry https://registry.example/jangolova.json
cymonkey modules pull \
  --registry https://registry.example/jangolova.json \
  --module render/blender --platform linux-amd64 --cache /var/cache/jangolova
```

The pull command returns a verified cache path. A provider or host must still
explicitly mount that artifact into a caller-owned target.

Browser extensions use the same conceptual flow but ship a reviewed package
registry inside the signed extension artifact. The extension must not download
and execute arbitrary JavaScript at runtime. Native modules such as Blender,
Godot, Unity, and Unreal are pulled by the Cymonkey host/provider for the
target platform.

## Explicit SDK activation

`Activate(ctx, module, platform, cachedPath, Activation{Approve, Mount})`
re-verifies the cached bytes, requires a host approval callback, and passes the
verified bytes to the runtime-specific mount callback. It then checks the
module hello, capabilities, description, and health. An incompatible module is
disconnected. The host remains responsible for per-call approval and target
lifecycle. See the [public SDK guide](../lib/jangolova/sdk/README.md).

The reference snapshot contains source-package metadata, not published artifact
URLs or proof that an engine has run. Its action lists follow current package
implementations; runtime negotiation is authoritative. The local Blender live
fixture supplies its own real module artifact and local test registry. See the
[validation record](jangolova-runtime-validation.md).
