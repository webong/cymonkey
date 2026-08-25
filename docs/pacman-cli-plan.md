# Plan: Pacman becomes the Jangolova CLI/TUI; Grimlock is API-only

Status: approved direction, not yet implemented. This file is the executable
brief for the implementing agent. Read docs/blockade-handoff.md for repository
context first and inspect the working tree before changing anything.

## Decision

The product splits into two named planes:

- **Grimlock** — the remote API plane only: the authenticated agent service
  and its protocol adapters (HTTP, MCP, ACP). No human-facing client commands.
- **Pacman** — the local client plane: the CLI today, a TUI next. Every
  operator and diagnostic command moves here under the entry name `pacman`.

The entry names are literal: `pacman engines`, `pacman blockade validate`,
`pacman tui`. Do not require the `jangolova` prefix on the client plane.

Known trade-off, accepted deliberately: a bare `pacman` command collides with
Arch Linux's package manager on PATH. Mitigation lives in the rollout notes
below (documented install name, `--version` banner identifying Jangolova), not
in renaming.

## Target naming

| Today                              | Tomorrow                                   | Plane   |
| ---------------------------------- | ------------------------------------------ | ------- |
| `jangolova engines`                | `pacman engines`                           | pacman  |
| `jangolova connect-engine`         | `pacman connect-engine`                    | pacman  |
| `jangolova models`                 | `pacman models`                            | pacman  |
| `jangolova connect-model`          | `pacman connect-model`                     | pacman  |
| `jangolova blockade validate/observe` | `pacman blockade validate/observe`      | pacman  |
| `jangolova serve-grimlock`         | `jangolova grimlock serve --http`          | grimlock|
| `jangolova serve-grimlock-mcp`     | `jangolova grimlock serve --mcp`           | grimlock|
| `jangolova serve-grimlock-acp`     | `jangolova grimlock serve --acp`           | grimlock|
| `jangolova gl [--mcp|--acp]`       | removed (superseded by the line above)     | grimlock|
| `jangolova serve-engine-provider`  | `jangolova engine-provider serve`          | grimlock|
| —                                  | `pacman tui`                               | pacman  |

Rules:

- One repo still builds two binaries: `cmd/jangolova` (server plane) and
  `cmd/pacman` (client plane). Shared logic stays in `internal/`; neither
  binary imports the other's `main`.
- Old top-level client commands remain as deprecated aliases for one compat
  window: they execute the new behavior unchanged and print a one-line notice
  to stderr (`deprecated: use pacman engines`). Server aliases
  (`serve-grimlock*`, `serve-engine-provider`) behave the same way.
- `gl` is removed immediately in favor of `jangolova grimlock serve`; it was
  an undocumented ergonomic alias with no external contract.

## Phase 1 — Pacman CLI

1. Create `cmd/pacman/main.go` dispatching subcommands:
   `tui` (Phase 3 stub), `engines`, `connect-engine`, `models`,
   `connect-model`, `blockade`. Move the existing implementations from
   `cmd/jangolova/*.go` into shared files under `internal/cli/` (package
   `cli`) so both binaries call them; keep function signatures stable.
2. `pacman` usage output groups commands as: Sessions (future), Engines,
   Models, Observation (blockade), plus `pacman --version` printing a banner
   that identifies it as the Jangolova client.
3. On the `jangolova` binary, replace the moved command bodies with
   deprecated aliases that forward to the same `internal/cli` functions and
   print the deprecation notice. Update `usage()` accordingly.
4. Delete the `gl` alias and its dispatch case.

Acceptance: `go build ./...`; `pacman engines`, `pacman models`,
`pacman blockade validate --config deploy/blockade/blockade.example.yaml`
behave exactly as before; old spellings still work and print the notice;
`go test ./...` green including `internal/boundary`.

## Phase 2 — Grimlock server consolidation

1. Implement `jangolova grimlock serve` accepting `--http`, `--mcp`, `--acp`
   (mutually exclusive; default http) by forwarding to the existing serve
   implementations. Keep `serve-grimlock*` as deprecated aliases forwarding
   to it.
2. Apply the same pattern to engine-provider: canonical
   `jangolova engine-provider serve`, deprecated alias
   `serve-engine-provider`.
3. Update `usage()` to present two planes explicitly: Client (pacman) and
   Server (grimlock, engine-provider).

Acceptance: every table row's "Tomorrow" spelling works; every deprecated
spelling works with its notice; no flag regressions (`--bind`,
`--session-store`, token env handling).

## Phase 3 — Pacman TUI

1. Add `pacman tui`: an interactive console that talks to a running Grimlock
   HTTP API (endpoint and token via the same environment variables the server
   documents, e.g. `JANGOLOVA_GRIMLOCK_ENDPOINT` if defined, else document the
   chosen variable in this file when implemented).
2. Scope, in order: list sessions; open a session; stream events; approve or
   deny pending actions; run one Blockade observation against a pasted or
   file-loaded image. Read-only first; destructive operations must reuse the
   server's approval flow, never bypass it.
3. Prefer a small dependency footprint. If a TUI framework is introduced,
   justify it in the PR description; stdlib-only is acceptable for phase one
   of the TUI.
4. The TUI is a client of the documented Grimlock API only. It must not link
   Grimlock internals to reach around the API.

Acceptance: TUI lists live sessions from a locally served Grimlock instance;
approvals round-trip through the pending-approval flow; `go test ./...` green.

## Phase 4 — Docs and skills sweep

1. Update every reference to old spellings in README.md and docs/
   (grep for `serve-grimlock`, `connect-model`, `jangolova engines`,
   `jangolova blockade`, `connect-engine`): display-interaction.md,
   grimlock.md, deployment-modes.md, target-connection-security.md,
   target-descriptor.md, cymonkey.md, blockade-handoff.md,
   next-chat-handoff.md.
2. Check `skills/` for any command spellings and update them.
3. Add a short section to README.md introducing the two planes: pacman =
   local CLI/TUI client, grimlock = remote agent API.
4. Only after the compat window elapses (separate change): delete the
   deprecated aliases from `cmd/jangolova`.

## Guardrails

- Pure rename/move for Phase 1–2: no behavior, flag, or env changes beyond
  what the table specifies. Any drift discovered while moving code gets fixed
  in place, not redesigned.
- The boundary test and Xallet ownership rules are untouched.
- Blockade, Cymonkey merge work (docs/cymonkey-display-plane-merge.md), and
  Grimlock service internals are out of scope here except where usage strings
  mention them.
- Never print tokens; deprecation notices go to stderr so scripts parsing
  stdout stay unaffected.

## Definition of done

- `pacman` is the only spelling documented for client work; `jangolova` serves
  only the API planes; `gl` is gone.
- Both binaries build from their own `cmd/` directories sharing
  `internal/cli`.
- All tests pass, including boundary and conformance suites.
