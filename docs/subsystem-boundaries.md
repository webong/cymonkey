# Cymonkey, Jangolova, and Blockade boundaries

Jangolova provides display interfaces. Blockade provides inference interfaces
for image and sound across local and cloud backends. Cymonkey is the operator,
entry, and extension layer that composes these modules. Blockade's current
public request is image-based; sound inference has not been implemented.
The libraries are standalone: Cymonkey depends on their public APIs; neither
library depends on Cymonkey or on the other library.

```text
caller or agent
  → Cymonkey authorizes and coordinates extensions to the system
    → Jangolova operates or presents on caller-owned displays and runtimes
    → Blockade runs local or cloud inference and returns evidence
```

## Cymonkey

Cymonkey discovers, installs, mounts, and coordinates access to a runtime. In
a browser it provides libraries and interfaces for caller-owned extensions,
reviewed packages, target-tab selection, approval, and authenticated routing.
It can host standalone Jangolova and Blockade processes. It does not implement
scene semantics, inspect arbitrary Three.js objects, or embed inference engines.

## Jangolova display interfaces

Jangolova owns the libraries and adapters that run *inside* a target runtime:
Three.js, Godot, Unity, Unreal, Blender, browser interaction, and future presentation
hosts. A library registers stable resources and an allowlist of semantic
operations through the shared bridge contract. It owns the translation from a
semantic request to a safe runtime-native operation.

A Jangolova runtime library can also be used without Cymonkey when a caller
already has a suitable runtime endpoint. Cymonkey is the normal way to enter
and coordinate that runtime, not a requirement baked into the library.

## Blockade inference interfaces

Blockade owns local and cloud inference integrations. Its current image request
receives pixels and returns normalized observations. Sound is part of its
inference scope but needs a separate public contract. Cymonkey may coordinate
a Jangolova screenshot with Blockade, but neither Jangolova nor Blockade
imports or configures the other.

Provider APIs and VLMs are Blockade adapter integrations, not local engines.
Blockade owns their configuration, credentials, and normalization into its
observation contract, while Cymonkey exposes the coordinated
screenshot-to-observation capability. This keeps Blockade deployable as a
standalone inference service and Jangolova limited to runtime interaction and
presentation libraries.
