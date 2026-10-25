# Published ext consumer acceptance

Run from a Cymonkey checkout with Python 3.12+, Go 1.26.5+ and Node.js on PATH:

```sh
python3 scripts/acceptance/published-ext.py --revision HEAD
```

The default tests the exact committed revision, not local edits. To review an
uncommitted acceptance change, use:

```sh
python3 scripts/acceptance/published-ext.py --working-tree
```

If Node is installed outside PATH, add `--node /absolute/path/to/node`.
Network access to the Go module proxy/checksum service and permission to bind
loopback test servers are required. No credentials or browser installation are
required for the deterministic integration tests.

The command exports Git sources into a new `/tmp/cymonkey-published-ext-*`
directory (on macOS, `/private/tmp`), outside the development workspace. Each
run uses fresh module/build caches and a fresh home. It sets `GOWORK=off`,
`GOENV=off`, `GOFLAGS=-mod=readonly`,
`GOPROXY=https://proxy.golang.org,direct`, and `GOSUMDB=sum.golang.org`.
Private-module exceptions and Git system/global URL rewrites are disabled.
No credentials, local Go environment overrides, or application configuration
are inherited. PATH is retained for installed toolchains and Node.

Every tracked nested Go module is inventoried and built/tested independently;
root `go test ./...` alone is insufficient. The export rejects vendor trees,
ext replacements, nonlocal replacements, escaping replacement paths, and
symlinks escaping the export. Legitimate internal replacements such as
`jangolova => ./lib/jangolova` are preserved. Dependency files are read-only and
compared byte-for-byte after the run. Tests use `-count=1`.

The exact remote module graph is also checked with `go mod verify` in a
separate generated verification module. Go 1.27.1's verifier attempts to hash
local short-name replacements (`jangolova v0.0.0`, for example) and reports a
missing ziphash. The verification module excludes only local replacements and
Go's synthetic `go`/`toolchain` entries. It keeps all remote selected versions;
local source integrity is recorded separately. This does not rewrite any
Cymonkey dependency file or substitute any ext source.

## Runtime coverage

The suite includes Cymonkey's production host paths, with real compiled and
installed guest executables:

- `TestContentCommandsRunInTheInstalledContentService`: install the Console
  content service through `pluginsCommand`, execute configuration commands,
  propagate a missing-file failure, upgrade to another revision, reject a
  same-revision upgrade while retaining the usable installation, and reject
  a tampered service executable.
- `TestContentServeStreamsItsLogsAndStopsWhenCanceled`: run the real content
  service through `runContentContext` and ext's progress protocol; receive its
  readiness log while it is running; perform an HTTP observation through the
  service and installed deterministic vision provider; cancel, reclaim the
  listening socket, reopen on the same address, then expire a deadline and
  reclaim the socket again.
- Existing Board display/device and gateway integration tests build and load
  the real packages. Browser management tests exercise trusted/untrusted
  adapter admission, operation validation, progress, failure and timeout
  through the actual Cymonkey browser host with deterministic adapters.

The vision provider and cooperative browser/native endpoints are fixtures.
This proves package/host interoperability and consumer lifecycle behavior, not
model quality, real-browser compatibility, native device access, network media
streaming, or live GPU/engine support. Stream coverage here means command log
progress, not a generated-model token stream. Socket reuse demonstrates server
resource release; it is not a whole-system process/descriptor leak audit.
Package digest verification is not publisher signature verification. Concurrent
hot upgrades, rollback across changed contracts, publisher authentication and
platform-specific installer behavior are not covered by this acceptance check.
Live tests that self-skip are listed in `skipped-tests.json`; they do not become
acceptance claims merely because the overall Go suite succeeds.

## Evidence

The command prints and retains its evidence directory even on failure:

- `result.json`: revision, overlay flag, each check's exit code, final result.
- `source.tar`: committed source export; `working-tree.patch` and
  `untracked.json` describe an optional overlay.
- `source-sha256.json`: exact tested file content hashes.
- `modules.json`, `environment.json`, `logs/toolchain.log`, and
  `logs/node-version.log`: module inventory and execution environment.
- `logs/*-graph.log`: complete `go list -m -json all` output per module;
  graph diagnostics are kept separately in `.stderr` files.
- `logs/*-build.log`, `logs/*-test.log`, `logs/*-verify.log`: build, runtime
  assertions, skipped cases and checksum verification evidence.
- `cymy` and `logs/cymy-help.log`: built executable and its startup smoke check.

Any failed build, test, graph resolution, verification, or dependency-file
change fails the command. Keep the full evidence directory when reporting a
failure; do not replace dependencies with an adjacent ext checkout to make it
pass. No releases or tags are created.
