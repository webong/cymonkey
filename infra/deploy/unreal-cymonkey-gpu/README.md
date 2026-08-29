# Unreal Cymonkey GPU fixture

This image is the packaged Unreal 5.8 conformance fixture built from
`pkg/unreal-cymonkey` and `tests/unreal-cymonkey-fixture`. It is intended for
headless CI, RunPod, or another Linux `amd64` container host. It is separate
from the distributable plugin: use the [plugin README](../../pkg/unreal-cymonkey/README.md)
when integrating Cymonkey into your own Unreal project.

## Pull the published image

```sh
docker pull ghcr.io/webong/jangolova/unreal-cymonkey-gpu:5.8
```

The image contains the cooked fixture and starts it through
`/usr/local/bin/run-unreal-cymonkey-gpu`. It exposes TCP port `8090` and writes
runtime artifacts, logs, and an optional `nvidia-smi` snapshot to
`/workspace/artifacts`.

## Run headless

The default uses Xvfb, so it can run without a physical display. This provides
an off-screen graphics context, but is not proof of hardware acceleration:

```sh
mkdir -p "$PWD/unreal-artifacts"
docker run --rm \
  --name jangolova-unreal-cymonkey \
  -p 8090:8090 \
  -e JANGOLOVA_CYMONKEY_TOKEN='replace-with-a-runtime-secret' \
  -v "$PWD/unreal-artifacts:/workspace/artifacts" \
  ghcr.io/webong/jangolova/unreal-cymonkey-gpu:5.8
```

For an NVIDIA host, add the GPU runtime and disable Xvfb when the host provides
the required display/Vulkan setup:

```sh
docker run --rm --gpus all \
  -p 8090:8090 \
  -e JANGOLOVA_CYMONKEY_TOKEN='replace-with-a-runtime-secret' \
  -e UNREAL_USE_XVFB=0 \
  -e DISPLAY=:0 \
  ghcr.io/webong/jangolova/unreal-cymonkey-gpu:5.8
```

The token is a container runtime secret. Never bake it into an image or commit
it to a compose file. The fixture's registered resource is `object:fixture`;
the source fixture documents its available actions and expected conformance
behavior.

## Build the image yourself

The build requires an approved Unreal 5.8 Linux development image containing
`RunUAT.sh` and a runtime image. Epic's Unreal images require the appropriate
account access and EULA acceptance:

```sh
docker login ghcr.io
docker build --progress=plain --platform linux/amd64 \
  --build-arg UE_BUILD_IMAGE=ghcr.io/epicgames/unreal-engine:dev-slim-5.8.0 \
  --build-arg UE_RUNTIME_IMAGE=ghcr.io/epicgames/unreal-engine:runtime \
  --build-arg UE_ROOT=/home/ue4/UnrealEngine \
  -f deploy/unreal-cymonkey-gpu/Containerfile \
  -t ghcr.io/your-org/jangolova/unreal-cymonkey-gpu:5.8 .
```

The build compiles the plugin, creates the editor receipt required by Unreal's
cook step, packages the fixture, and installs Xvfb in the runtime layer.
