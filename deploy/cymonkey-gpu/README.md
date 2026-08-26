# Pacman GPU fixture images

These images are render-capable target environments for GPU-backed CI, such as
a RunPod Pod. They are separate from the CPU/headless fixtures and do not
bundle proprietary Unity or Unreal binaries.

Each image:

- targets `linux/amd64` GPU hosts;
- advertises the NVIDIA container runtime through
  `NVIDIA_VISIBLE_DEVICES=all` and `NVIDIA_DRIVER_CAPABILITIES`;
- writes engine logs and an `nvidia-smi` snapshot to `/workspace/artifacts`;
- exposes the conventional Pacman port `8090/tcp`;
- supports an operator-provided `DISPLAY` or an Xvfb fallback.

The Xvfb fallback makes the fixture runnable without a display server, but it
is not evidence of hardware-accelerated rendering. For real GPU rendering,
provide a display/EGL/Vulkan setup supported by the engine and set the
corresponding `*_USE_XVFB=0` variable. Pacman authentication tokens are always
runtime secrets.

## Quick start

The complete base-image, build, push, and RunPod handoff procedure is below.
Unity license activation and Unreal Engine binaries must be supplied through
the operator's approved image or runtime secret mechanism; they are never
stored in this repository.

## Build and push handoff workflow

The deployment handoff is a set of image references, not an uploaded license
file. Build or obtain the licensed base images first, push them to a private
registry, then build and push the Pacman layers that reference those bases.

Set the registry and version in your shell. `docker login` should prompt for
credentials; do not put a registry token in a command, Dockerfile, or build
argument.

```sh
export REGISTRY=ghcr.io/YOUR_ORG
export VERSION=2022.3
docker login ghcr.io
```

### Publish the Unity base

Start from an approved Linux `x86_64` Unity 2022.3 Editor image or build one
from an authorized Linux Editor installation. Verify that the required
executable is present:

```sh
docker run --rm \
  --entrypoint /opt/unity/Editor/Unity \
  "${REGISTRY}/unity-editor:${VERSION}" \
  -batchmode -nographics -quit -logFile -
```

If the base image is currently local under another name, tag and push it:

```sh
docker tag local/unity-editor:2022.3 "${REGISTRY}/unity-editor:${VERSION}"
docker push "${REGISTRY}/unity-editor:${VERSION}"
```

The Unity license is activated later through a protected runtime mechanism;
it is not copied into this base image during the handoff.

### Publish the Unreal bases

Build or obtain an approved Linux Unreal build image and a smaller runtime
image. Verify that the build image contains `RunUAT.sh`:

```sh
docker run --rm \
  --entrypoint /opt/UnrealEngine/Engine/Build/BatchFiles/RunUAT.sh \
  "${REGISTRY}/unreal-build:5.3" -Help
```

Tag and push local images when they are ready:

```sh
docker tag local/unreal-build:5.3 "${REGISTRY}/unreal-build:5.3"
docker tag local/unreal-runtime:5.3 "${REGISTRY}/unreal-runtime:5.3"
docker push "${REGISTRY}/unreal-build:5.3"
docker push "${REGISTRY}/unreal-runtime:5.3"
```

### Build and push the Pacman layers

Run these commands on the Mac with OrbStack or on the Debian `x86_64` build
server:

```sh
docker build --progress=plain --platform linux/amd64 \
  --build-arg GODOT_IMAGE=barichello/godot-ci:4.3 \
  -f deploy/godot-cymonkey-gpu/Containerfile \
  -t "${REGISTRY}/jangolova-godot-cymonkey-gpu:4.3" .

docker build --progress=plain --platform linux/amd64 \
  --build-arg UNITY_EDITOR_IMAGE="${REGISTRY}/unity-editor:${VERSION}" \
  -f deploy/unity-cymonkey-gpu/Containerfile \
  -t "${REGISTRY}/jangolova-unity-cymonkey-gpu:${VERSION}" .

docker build --progress=plain --platform linux/amd64 \
  --build-arg UE_BUILD_IMAGE="${REGISTRY}/unreal-build:5.3" \
  --build-arg UE_RUNTIME_IMAGE="${REGISTRY}/unreal-runtime:5.3" \
  --build-arg UE_ROOT=/opt/UnrealEngine \
  -f deploy/unreal-cymonkey-gpu/Containerfile \
  -t "${REGISTRY}/jangolova-unreal-cymonkey-gpu:5.3" .

docker push "${REGISTRY}/jangolova-godot-cymonkey-gpu:4.3"
docker push "${REGISTRY}/jangolova-unity-cymonkey-gpu:${VERSION}"
docker push "${REGISTRY}/jangolova-unreal-cymonkey-gpu:5.3"
```

For a private registry, configure its read credential in RunPod's registry
settings rather than placing the credential in an image or command line.

### Handoff manifest

The non-secret text needed for deployment is just the final image manifest:

```env
UNITY_EDITOR_IMAGE=ghcr.io/YOUR_ORG/jangolova-unity-cymonkey-gpu:2022.3
UE_BUILD_IMAGE=ghcr.io/YOUR_ORG/jangolova-unreal-cymonkey-gpu:5.3
UE_RUNTIME_IMAGE=ghcr.io/YOUR_ORG/unreal-runtime:5.3
UE_ROOT=/opt/UnrealEngine
GODOT_IMAGE=ghcr.io/YOUR_ORG/jangolova-godot-cymonkey-gpu:4.3
```

Provide the image names, tags, engine versions, and registry location. Supply
registry read credentials and Unity license secrets directly to the target
environment; never send those secrets in chat.

## Supplying the licensed engine bases

The `Containerfile`s use `ARG` values for the engine bases so the proprietary
editor/build layers stay outside this repository.

### Unity

Provide a private Linux `x86_64` Unity 2022.3 Editor image with this layout:

```text
/opt/unity/Editor/Unity
/opt/unity/Editor/Data/...
```

The executable must be runnable by the configured `UNITY_CONTAINER_USER`.
Build or obtain that base image using Unity's licensing terms, then push it to
a private registry. Do not put a serial, account password, `.ulf` license, or
floating-license credential in a Dockerfile, build argument, or image layer.

Unity can be activated outside the image at runtime using an approved
floating-license server or a protected mounted license file. Unity documents
manual activation with `-createManualActivationFile` and
`-manualLicenseFile`; those files should be handled as secrets and removed
after the runner is retired. [Unity command-line licensing](https://docs.unity3d.com/es/current/Manual/CommandLineArguments.html)

Build the Pacman GPU image after the private base is available:

```sh
docker login ghcr.io

docker build --platform linux/amd64 \
  --build-arg UNITY_EDITOR_IMAGE=ghcr.io/ORG/unity-editor:2022.3 \
  --build-arg UNITY_CONTAINER_USER=root \
  -f deploy/unity-cymonkey-gpu/Containerfile \
  -t ghcr.io/ORG/jangolova-unity-cymonkey-gpu:2022.3 .

docker push ghcr.io/ORG/jangolova-unity-cymonkey-gpu:2022.3
```

When RunPod pulls a private image, configure its private registry credential
in RunPod rather than embedding a registry token in the image or command line.

### Unreal Engine

Provide two private Linux `x86_64` images:

```text
UE build image:    /opt/UnrealEngine/Engine/Build/BatchFiles/RunUAT.sh
UE runtime image:  compatible packaged-game runtime libraries
```

The build image must contain the approved Unreal Engine installed build or
source build and its Linux toolchain. The runtime image should contain only the
libraries needed by the packaged game. Epic documents using Linux Unreal
containers and `RunUAT.sh BuildCookRun` for packaging. [Epic Unreal container quickstart](https://dev.epicgames.com/documentation/unreal-engine/quick-start-guide-for-using-container-images-in-unreal-engine)

Keep the Unreal images in a private registry and comply with the Unreal Engine
EULA. Do not copy the engine into this repository or publish it in a public
image. Build the Pacman GPU image with:

```sh
docker login ghcr.io

docker build --platform linux/amd64 \
  --build-arg UE_BUILD_IMAGE=ghcr.io/ORG/unreal-build:5.3 \
  --build-arg UE_RUNTIME_IMAGE=ghcr.io/ORG/unreal-runtime:5.3 \
  --build-arg UE_ROOT=/opt/UnrealEngine \
  -f deploy/unreal-cymonkey-gpu/Containerfile \
  -t ghcr.io/ORG/jangolova-unreal-cymonkey-gpu:5.3 .

docker push ghcr.io/ORG/jangolova-unreal-cymonkey-gpu:5.3
```

### Godot

Godot is the license-free case. The public base used by the repository can be
built without a private editor or activation secret:

```sh
docker build --platform linux/amd64 \
  --build-arg GODOT_IMAGE=barichello/godot-ci:4.3 \
  -f deploy/godot-cymonkey-gpu/Containerfile \
  -t ghcr.io/ORG/jangolova-godot-cymonkey-gpu:4.3 .

docker push ghcr.io/ORG/jangolova-godot-cymonkey-gpu:4.3
```

## Where to build

The image build and the GPU test run are separate operations:

| Location | Build images | Run real GPU rendering | Notes |
| --- | --- | --- | --- |
| Mac + OrbStack | Yes, with `--platform linux/amd64` | No NVIDIA Linux GPU | Apple Silicon may emulate AMD64 and build slowly. |
| Debian CPU server | Yes | No | The current 16-vCPU/64-GB host is suitable for Godot and Unity builds; Unreal needs substantial free disk and build time. |
| RunPod GPU Pod | Yes, but unnecessary | Yes | Prefer building once, pushing to a registry, then using the Pod for rendering. RunPod Pods run a custom image and do not provide Docker Compose inside the Pod. |

On the Mac or CPU server, use the existing headless fixtures for reliable CPU
validation. A GPU image may use software rendering through its Xvfb fallback,
or fail if the base lacks compatible display libraries; that does not count as
hardware-rendered evidence. For real GPU validation, deploy the pushed image to
RunPod with an NVIDIA GPU, set the engine's `*_USE_XVFB=0` when a real display or
EGL/Vulkan setup is supplied, and retain `/workspace` for the fixture source.
Mount persistent RunPod storage at a separate path such as
`/mnt/runpod-volume`, then set (for example)
`UNREAL_ARTIFACT_DIR=/mnt/runpod-volume/artifacts`.

## RunPod settings

Use a secure GPU Pod with a 24 GB-or-larger GPU when available, persistent
storage for engine caches/artifacts, `8090/tcp` exposed, and
`JANGOLOVA_PACMAN_TOKEN` injected as a secret. Mount the persistent volume at a
path such as `/mnt/runpod-volume`; do not mount it over `/workspace`, because
the image's fixture source is copied there. Set the engine-specific artifact
variable to `/mnt/runpod-volume/artifacts` when artifacts must survive Pod
restarts.

RunPod Pods run one custom image; Docker Compose is not part of this deployment
model. Keep the Pacman listener authenticated and prefer a private tunnel or
TLS endpoint over an open public port. The current Unity and Unreal fixtures
exercise their package conformance entrypoints; their live WebSocket listeners
remain a separate integration layer, while the Godot fixture owns the initial
listener implementation.
