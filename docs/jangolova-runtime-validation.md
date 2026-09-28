# Jangolova runtime validation — 2026-09-06

## Implemented boundary

The integration at `lib/jangolova` now uses the public wire contract in
`lib/jangolova/contract` and public host/module types in `lib/jangolova/sdk`.
Its production dependency graph contains no private Cymonkey packages.
The old private wire files were moved to the public contract package; the wire
identifier remains `cymonkey/v1alpha1` for compatibility.

`src/internal/hostbinding` supplies Cymonkey's private worker, credential, listener,
and Safari services through SDK ports and explicitly converts target/session
data. It preserves credential revision notifications and acknowledgements.
The operator still owns policy and approval. Jangolova owns runtime modules and
semantic operations. Blockade owns visual inference and cloud/VLM adapters.

External modules register `jangolova.Backend` implementations on
`Adapter.Backends`; all signature types, including `Options`, are public.
Missing required host services fail explicitly.

`registry.Activate` now requires approval and a mount callback, verifies the
cached artifact immediately before mounting, passes verified bytes rather than
a mutable path, and checks hello/capabilities/describe/health afterward. Failed
negotiation disconnects the module. Approval and mounting receive independent
metadata copies. The checked-in catalog's action lists now reflect actual
package implementations rather than a generic list that every engine was
assumed to support.

## Real Blender rendering

Test host: Linux amd64 VPS `159.195.214.107`. No GPU was used.
Image: `jangolova/blender-sdk-fixture:debian13`, built on that host; not published.
Observed image ID: `sha256:f98cd2a8bf3a24f8f9fcffdd4cffdadc46873e7bd5949d2c617d86faef60288d`.
Observed engine: Blender 4.3.2, Cycles CPU, 24 samples, 384×384 PNGs.

The caller fixture served the actual Blender Python module through a local
HTTP registry, pulled it through the registry client, and mounted the verified
bytes only after explicit approval. Blender imported that mounted module.
The socket used a runtime token, with the host port bound only to loopback.

Passed checks:

- Discovery and SHA-256 artifact verification.
- No activation without an approval callback.
- `hello`, `capabilities`, `describe`, `health`, `act`, and cursor-based `events`.
- Invalid token rejected.
- Unregistered resource rejected; per-resource action allowlist enforced.
- Unsupported semantic method rejected.
- Actual before/after renders with different decoded-pixel hashes after moving
  the registered roof.
- Detach preserved Blender; a new authenticated connection remained healthy.

Artifact SHA-256:
`913e915dd6008de63f4e7cd142241d83c7a5de6efe529256e28832bbd525162b`.

Before decoded-pixel SHA-256:
`481496468bdbf35afaede4f3be95bc0c8eacf3cb032116aa726654fd412b76f9`.

After decoded-pixel SHA-256:
`385eaaea813b4a44754bf649dec2ed38dfb38ded23e073ec89d0bf524362e956`.

The images were also visually inspected: the roof is on the house before the
action and displaced above/to the side afterward. These are engine renders,
not mocks, browser screenshots, or generated illustrations.

Remote artifacts: `/opt/jangolova/blender-sdk-7nEFhM/artifacts-final`.
Local copies: `.cache/blender-sdk-validation/` (ignored by Git).
Files: `before.png`, `after.png`, `events.json`, `runtime.json`, `report.json`,
and `blender.log`. The caller stopped its test container after verification;
the reusable image and artifacts remain.

The first render attempt exposed unavailable OpenImageDenoise support in the
Debian package. Denoising was disabled in the fixture; the final run passed.
See [reproduction instructions](../tests/blender-cymonkey-fixture/README.md).

## Automated checks

Passed focused race checks:

```sh
go test -race ./lib/jangolova ./lib/jangolova/... \
  ./src/internal/hostbinding ./src/internal/builtin ./src/internal/engineprovider \
  ./src/internal/provider ./pkg/lens-studio-cymonkey
```

Includes public dependency enforcement, external backend registration, explicit
approval denial, cache tampering and byte limits, immutable mount input,
negotiation cleanup, host credential rotation, and event/call conversion.

The generic `cymonkey` build from `./src` passed.
The module registry interface is exposed as `cymonkey modules`.
Protocol-generation verification and `git diff --check`
passed. Seven Node checks passed: core conformance, engine runtime manifest,
module registry, Blender fixture contract, Godot fixture contract, Unity package
contract, and Unreal package contract. These Node checks are static/contracts;
they do not run those engines.

The separate browser contract suite had 7 passes and one existing failure:
`tests/cymonkey-contract-test.mjs:82` expects `browser.disconnect()` after
`disableInterception()`. The current worker intentionally releases its worker
connection and sets `browser = null` to preserve the Playwright target. The
Augmented Browsing task owns that assertion and its live browser tests.

## Remaining integration work

- Three.js product flow and live browser fixture belong to the Augmented
  Browsing task. This record does not claim its rendering result.
- Lens Studio's separate task consumes the public Backend seam; its mocked
  package tests pass here. No Lens Studio application was run in this work.
- Unity/Unreal/Godot were not run for this change. Their contract checks passed;
  current binaries, licensing, and platform behavior still require live checks.
- macOS/Windows helper tests use fixtures; the optional real Swift helper was
  not configured. No native desktop or GPU rendering validation is claimed.
- Camera Kit's former `lifetime: document` mismatch was corrected to `surface`
  during the augmentation stability pass. A shared-schema regression check
  covers its descriptor; this does not claim live camera/media validation.
- SDK publication as an independent Go module, signed production registry
  metadata, and published runtime artifacts remain release work. The `cymonkey modules` interface
  supports discovery/pull; runtime-specific activation is an explicit host API,
  not a universal process or binary-plugin launcher.

This boundary change excludes unrelated browser/product and Lens Studio work.
Its architecture-document update covers only the public-contract and
host-binding path map. The old `jangolova` checkout was not modified.
