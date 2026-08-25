# Plan: Cymonkey owns the semantic display plane (Pacman contract merge)

Status: approved direction, not yet implemented. This file is the executable
brief for the implementing agent. Work through the phases in order; do not
skip the verification gates. Read docs/blockade-handoff.md for repository
context before starting, and inspect the working tree first because unrelated
concurrent changes may exist.

## Decision

Cymonkey becomes the single owner of the semantic display plane — interaction
and presentation — for every target runtime. Web pages, macOS surfaces, and
2D/3D game engines all speak one core contract with per-runtime profiles.
`jangolova.pacman/v1alpha1` stops being an independent protocol; it becomes a
compatibility alias of the Cymonkey engine profile during migration.

This matches the existing trajectory: docs/cymonkey-runtime.md already defines
the portable `jangolova.cymonkey/v1alpha2` core whose profiles implement the
same five operations (`hello`, `capabilities`, `describe`, `act`, `events`)
that Pacman uses today, and it already lists "Pacman presentation drivers"
among backends.

## Target shape

```
jangolova.cymonkey/v1alpha2 core            one contract, five operations
├── web profile          extension/userscript transport   (exists)
├── macos profile        Accessibility transport          (in progress)
└── engine profile       Godot/Unity/Unreal transport     (was Pacman)
```

Core-contract requirements promoted from Pacman (they become mandatory for
every profile):

- Strictly allowlisted semantic resources with revision counters in
  `describe`.
- Exact version negotiation in `hello`, including implementation identity.
- No scene-tree, UObject-heap, or UI-tree scanning. Ever. Only explicitly
  registered resources are addressable.
- Bounded capability schemas describing effects before actions run.

## Naming policy

- The wire name `jangolova.pacman/v1alpha1` is deprecated but continues to
  work behind an adapter for one compat window.
- The `pacman` name survives only as the packaging brand for engine fixture
  images (`ghcr.io/webong/jangolova/unreal-pacman-gpu:5.8`,
  `deploy/*-pacman-gpu`) so published images and existing skills stay valid.
- Once all shipped images are rebuilt against the engine profile, retire the
  pacman name entirely in a follow-up change. Do not reserve the name for
  speculative future subsystems.

## Phase 1 — Contract

1. Extend `protocol/cymonkey/v1alpha2/protocol.schema.json` and
   `augmentation.schema.json` with the engine profile: capability names,
   resource descriptors with revisions, and effect descriptions currently
   expressed in `protocol/pacman/v1/protocol.schema.json`.
2. Document profile identity rules in docs/cymonkey-runtime.md: how `hello`
   reports `profile: engine` plus engine kind (godot/unity/unreal) and
   implementation version.
3. Add a deprecation note to docs/pacman.md pointing here; keep its ownership
   prose (caller-owned target endpoint, disconnect never terminates
   processes) — that text is normative for the engine profile too.

Acceptance: both schemas validate; docs state the merge and the compat
window.

## Phase 2 — Go types and adapter

1. Add generated Cymonkey types for the engine profile alongside
   `internal/pacmanprotocol/generated_v1.go` (same generation style; keep the
   file gofmt-clean).
2. Implement an adapter that accepts `jangolova.pacman/v1alpha1` peers and
   speaks cymonkey/v1alpha2 internally:
   - translate `hello`/`capabilities`/`describe`/`act`/`events` envelopes,
   - preserve revision semantics exactly (a stale-revision act must fail),
   - surface pacman-native errors as their closest v1alpha2 errors without
     inventing new error taxonomies.
3. Keep `adapters/pacman` and `internal/pacman` behavior unchanged for
   existing callers; the adapter wraps rather than rewrites. Existing tests
   must still pass untouched.

Acceptance: table-driven conformance tests covering envelope translation,
revision rejection, and unknown-resource errors; `go test ./...` green.

## Phase 3 — Callers and fixtures

1. Switch `adapters/pacman/transport.go` construction sites to prefer the
   v1alpha2 path, falling back to the pacman adapter when the peer negotiates
   v1alpha1.
2. Engine fixtures under `deploy/godot-pacman-gpu`, `deploy/unity-pacman-gpu`,
   `deploy/unreal-pacman-gpu`, and `deploy/*-pacman-fixture` keep their image
   names and ports; only their embedded protocol listener changes when each
   runtime is ready. Migrate one engine at a time; Godot first since it is
   the license-free reference.
3. Update the two skills under `skills/jangolova-pacman-*` only where they
   name the wire protocol; image names and setup flows stay.

Acceptance: `tests/` fixture contract checks pass; one engine fixture
completes a hello/capabilities/describe/act/events round over v1alpha2 while
another still passes over v1alpha1 through the adapter.

## Phase 4 — Cleanup gate (separate change)

Only after all shipped images negotiate v1alpha2 natively: delete the pacman
adapter, mark `internal/pacmanprotocol/generated_v1.go` deprecated, fold
remaining pacman docs into the engine-profile section, and remove the
deprecation notes. Do not bundle this into earlier phases.

## Guardrails

- The repository boundary test (`internal/boundary`) forbids display-runtime
  concepts in product code and deployment topology outside allowlisted paths.
  Run `go test ./internal/boundary` after every phase.
- Jangolova never owns placement, GPU, display transport, credentials, or
  process lifecycle; Xallet or another caller does. The merged contract must
  not grow fields that pull those responsibilities inward.
- Blockade and Grimlock interfaces are unaffected. Screenshot capture adapters
  (see docs/blockade-handoff.md next tasks) should target the unified
  abstraction once Phase 2 lands.
- No secrets, tokens, or endpoints in committed files or logs at any step.

## Definition of done

- One wire contract (`jangolova.cymonkey/v1alpha2`) serves web, macOS, and
  engine targets; pacman remains only as a negotiated compatibility alias and
  an image-brand name.
- All pre-existing tests, conformance suites, and boundary checks pass.
- docs/pacman.md either removed or reduced to a deprecation pointer, per the
  cleanup gate.
