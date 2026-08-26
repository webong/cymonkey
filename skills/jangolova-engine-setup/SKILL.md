---
name: jangolova-engine-setup
description: Provision and verify Jangolova Pacman engine integrations and headless container environments for Godot, Unity, and Unreal. Use when setting up a Pacman runtime, building or pulling an engine image, configuring a server, or preparing a RunPod or CPU test host.
---

# Jangolova Engine Setup

Use this skill when an agent must make an engine controllable by Jangolova. Keep engine binaries, licenses, registry credentials, and Pacman bearer tokens outside source control.

## Choose the runtime

- Godot: build `deploy/godot-cymonkey-gpu/Containerfile`; it is the license-free reference runtime.
- Unreal: pull `ghcr.io/webong/jangolova/unreal-pacman-gpu:5.8` for the packaged fixture, or install `pkg/unreal-cymonkey` from the GitHub Release for a custom project.
- Unity: supply a licensed private Unity Linux Editor base; never invent or embed Unity credentials.

Read [references/runtime-matrix.md](references/runtime-matrix.md) for engine-specific requirements.

## Standard setup sequence

1. Confirm a Linux `amd64` host, Docker, free disk, and, for rendered tests, an NVIDIA container runtime or supported display/EGL/Vulkan setup.
2. Authenticate to the registry interactively. Never put PATs in commands, Dockerfiles, build arguments, logs, or chat.
3. Pull or build the selected Pacman image.
4. Run it with a protected `JANGOLOVA_PACMAN_TOKEN`, port `8090`, and a mounted artifacts directory.
5. Verify `hello`, `capabilities`, `describe`, and `health` before attempting actions.

```sh
export JANGOLOVA_PACMAN_TOKEN="$(openssl rand -hex 32)"
mkdir -p artifacts
docker run --rm --name jangolova-pacman \
  -p 8090:8090 \
  -e JANGOLOVA_PACMAN_TOKEN \
  -v "$PWD/artifacts:/workspace/artifacts" \
  ghcr.io/webong/jangolova/unreal-pacman-gpu:5.8
```

For real GPU rendering add `--gpus all` and configure the engine display path. Xvfb is useful for off-screen smoke tests but does not prove hardware acceleration.

## Verification checklist

- The image tag and port are correct.
- Logs show no missing executable, token, display, or Vulkan error.
- `hello` reports `jangolova.cymonkey/v1alpha2` with the `engine` profile.
- `describe` contains only explicitly registered resources.
- An intentionally unallowlisted action is rejected.
- Logs and artifacts are written to the mounted directory.

Never scan an engine world, scene tree, UObject heap, or UI tree. Pacman control is explicit and allowlisted.
