# Cymonkey, Jangolova, and Blockade boundaries

Jangolova is the runtime-library layer. Cymonkey is the operator and entry
layer. Blockade is the observation layer.

```text
caller or agent
  → Cymonkey enters a caller-owned runtime and coordinates services
    → Jangolova runtime library exposes bounded semantic operations
      → Three.js / Unity / Unreal / Godot / browser surface
  → Blockade receives pixels only and returns observations
```

## Cymonkey

Cymonkey discovers, installs, mounts, and coordinates access to a runtime. In
a browser it supplies the extension, reviewed package registry, target-tab
selection, approval, and authenticated private routing. It can host standalone
Jangolova and Blockade processes. It does not implement scene semantics,
inspect arbitrary Three.js objects, or embed vision engines.

## Jangolova runtime libraries

Jangolova owns the libraries and adapters that run *inside* a target runtime:
Three.js, Unity, Unreal, Godot, browser interaction, and future presentation
hosts. A library registers stable resources and an allowlist of semantic
operations through the shared bridge contract. It owns the translation from a
semantic request to a safe runtime-native operation.

A Jangolova runtime library can also be used without Cymonkey when a caller
already has a suitable runtime endpoint. Cymonkey is the normal way to enter
and coordinate that runtime, not a requirement baked into the library.

## Blockade

Blockade receives pixels and returns normalized observations. Cymonkey may
coordinate a Jangolova screenshot with Blockade, but neither Jangolova nor
Blockade imports or configures the other.

Provider APIs and VLMs are Blockade adapter integrations, not local engines.
Blockade owns their configuration, credentials, and normalization into its
observation contract, while Cymonkey exposes the coordinated
screenshot-to-observation capability. This keeps Blockade deployable as a
standalone inference service and Jangolova limited to runtime interaction and
presentation libraries.
