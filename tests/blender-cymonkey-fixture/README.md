# Real Blender SDK validation

`fixture.py` supports a fake-object contract mode. That mode does not establish
that Blender rendered a frame. `live.py` requires real `bpy` and never falls back
to fake objects.

On a Linux Docker host, from the repository root:

```sh
docker build -f tests/blender-cymonkey-fixture/Containerfile \
  -t jangolova/blender-sdk-fixture:debian13 .
go run ./tests/jangolova-blender-live --root .
```

The caller fixture starts an isolated container, binds its port only to host
loopback, injects an ephemeral runtime token through the environment, and stops
its container after verification. The runtime module never stops Blender.
The output directory must be new; use `--output /absolute/new-directory` or
accept the generated temporary directory.

The test serves a local registry and the actual `pkg/blender/blender_cymonkey.py`
artifact. It checks discovery, SHA-256 retrieval, denial without approval,
explicit mounting of verified bytes, authenticated protocol negotiation,
registered object mutation, events, and per-resource action rejection.

Blender renders two 384×384 PNGs using Cycles on the CPU. The test decodes both
images, checks that they contain varied pixels, and compares decoded-pixel
hashes after moving the roof. It disconnects and reconnects before the caller
stops its container, verifying that the integration preserved the target.

Artifacts: `before.png`, `after.png`, `events.json`, `runtime.json`, `report.json`,
and `blender.log`. The fixture uses 24 samples and disables denoising because the
tested Debian Blender build does not include OpenImageDenoise.

This is a real CPU-rendering test, not GPU acceleration, desktop UI control,
video recording, or a Windows/macOS runtime test. The Debian package version is
recorded from the running engine; package repositories can change between image
builds.
