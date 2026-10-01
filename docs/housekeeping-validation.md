# Skill and runtime directory validation — 2026-09-29

This records validation performed on 2026-09-29. The Swift catalog and
managed-runtime controller are now part of `pkg/macos-helper`; run
`swift test --package-path pkg/macos-helper` for the current package.

## Completion checklist

- [x] Correct all five engine runner `COPY` paths.
- [x] Add regression coverage for every local input in the six affected engine Containerfiles.
- [x] Run the official validator for all four relocated skills in an isolated Python environment.
- [x] Run a Docker build-context smoke build and the affected package checks.
- [x] Record full-engine build prerequisites and any remaining limits.
- [x] Isolate housekeeping changes for this commit, preserving unrelated Blockade work.

Scope: the move from `skills/` to `.agents/skills/`, the four skill-folder
renames, and the nine shortened runtime directories under `pkg/`. The 92
relocated tracked files were compared with their originals. Apart from the
intentional skill UI labels and playbook heading, their contents were unchanged
before this review. Concurrent Blockade content and extension-signing changes
were not reviewed or modified by this pass.

## Findings corrected

- **P1 — Update package-path consumers when moving runtime directories.** npm
  scripts, test imports, engine Containerfiles, the Unity fixture's local
  dependency, and documentation still pointed at deleted directories. A focused
  pre-fix test run failed nine checks with missing-file/module errors. Consumers
  now resolve the shortened paths. Published package names, runtime identifiers,
  protocol versions, image tags, and existing test command names are unchanged.
- **P1 — Update the Swift local dependency and its product identity together.**
  The Swift catalog package still referred to the old helper directory and
  Swift package identity. Both were updated to `macos-helper` at the time.
- **P2 — Match skill identities and invocation prompts to the renamed folders.**
  All four entrypoints and UI default prompts still named the old skills.
  They now use `display-engine-control`, `display-engine-setup`,
  `provider-adapter`, and `tools`. The skill-creator naming and metadata guidance
  was used for these narrow corrections; behavioral instructions were preserved.

The follow-up also corrected all five pre-existing `COPY deploy/...` runner
paths to `COPY infra/deploy/...` and added local-input regression coverage.

## Verification

- `npm run test:repository-layout`: six passing checks for npm package paths,
  all local inputs in the six affected engine Containerfiles, COPY extraction,
  container package inputs and Unity dependencies, the Swift dependency, and
  skill names/invocations/reference links.
- `npm run test:engine-build-contexts`: real Docker BuildKit scratch build
  passes for all 11 distinct local COPY inputs from those six recipes. This
  tests Docker context transfer/copy, not engine compilation or execution.
  No engine images are downloaded, no engine process runs, and no image is tagged.
- Focused Node runtime/fixture suite: 17 passing checks, including two Three.js
  browser-bundle checks. Three.js typechecking and Camera Kit builds pass.
- The Swift catalog's three tests passed in its then-current package. They now
  run within `pkg/macos-helper`.
- `npm run test:cymonkey:macos-helper`: seven Swift tests and two contract checks
  pass. The moved helper's generated `.build` cache retained old absolute PCH
  paths; `swift package --package-path pkg/macos-helper clean` removed that
  rebuildable cache before a successful rebuild. No source was removed.
- `go test ./pkg/effect-house ./pkg/lens-studio ./pkg/windows-helper`: all three
  packages pass on macOS. This does not validate Windows-only execution.
- No stale references to the nine old `pkg/` paths remain in repository sources.
  `git diff --check` passes.

## Skill validation

All four skills pass the official skill-creator `quick_validate.py` validator.
The missing PyYAML dependency was supplied using `uv run --no-project --with
PyYAML==6.0.3 python`; neither global Python nor project dependencies were changed.
The repository layout suite separately checks skill names, default invocations,
and linked references. Neither check is an agent-behavior evaluation.

## Remaining environment-dependent validation

Full engine image builds and live Godot/Unity/Unreal execution remain gated.
The local Docker daemon has no engine base images cached, and the host had only
about 3.4 GiB free during this pass. Unity needs an owner-supplied licensed
`UNITY_EDITOR_IMAGE`; Unreal needs owner-supplied `UE_BUILD_IMAGE` and
`UE_RUNTIME_IMAGE`. Godot's base is public but was not downloaded with this
disk-space constraint. GPU checks additionally require a compatible GPU host.
The COPY-only smoke build is deliberately not a claim that the full recipes
compile, run, or render successfully.

The generic skill name `tools` is valid but less distinctive than the other
names. A domain-specific name can be considered separately; this pass preserves
the folder names chosen by the user. No registry publication or Git push is part
of this housekeeping pass.
