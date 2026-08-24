# Unity Pacman headless fixture image

This image runs the Unity Pacman fixture through a Linux Unity 2022.3 Editor in
`-batchmode -nographics`. It is a separate caller-owned target/test environment,
not an extension of the Jangolova provider image.

The repository does not select, download, or redistribute Unity Editor
binaries. Supply an approved private image whose Editor executable is at
`/opt/unity/Editor/Unity`, or override `UNITY_EDITOR_PATH` at runtime:

To build that private base image directly from a Linux Editor installed at
`/opt/unity` on the VPS:

```sh
cd /opt/unity
docker build \
  -f /path/to/jangolova/deploy/unity-pacman-fixture/UnityBase.Containerfile \
  -t private/unity-editor:2022.3 .
```

```sh
docker build \
  --build-arg UNITY_EDITOR_IMAGE=registry.example/unity-editor:2022.3 \
  --build-arg UNITY_CONTAINER_USER=root \
  -f deploy/unity-pacman-fixture/Containerfile \
  -t jangolova/unity-pacman-fixture:local .
```

Activate the Editor using the private base image's documented licensing
mechanism before running the fixture. Use a read-only mounted license file or a
network license server/floating entitlement. Never bake credentials, serials,
license contents, or account passwords into this image, its build arguments,
the repository, or the Unity command line.

Set `UNITY_CONTAINER_USER` to the user whose home directory contains the
activated license when the private base image does not run the Editor as root.

Run the headless fixture after license activation:

```sh
docker run --rm jangolova/unity-pacman-fixture:local
```

The runner writes Unity's detailed Editor output inside the ephemeral container
at `/tmp/unity-pacman-fixture.log` and emits only a secret-free pass/fail line to
container stdout. Override `UNITY_FIXTURE_LOG_PATH` when CI mounts a protected
test-artifact directory.
