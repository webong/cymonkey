# Unreal Cymonkey fixture image

This is a separate target-runtime image definition. It builds the Unreal
plugin and fixture project with an operator-supplied Unreal build image, then
copies only the packaged Linux game into an operator-supplied runtime image.
The Jangolova provider is not copied into either stage.

Unreal Engine images and licensing are intentionally not selected here. Supply
approved images and the engine path explicitly:

```sh
docker build \
  --build-arg UE_BUILD_IMAGE=registry.example/unreal-build:5.3 \
  --build-arg UE_RUNTIME_IMAGE=registry.example/unreal-runtime:5.3 \
  --build-arg UE_ROOT=/opt/UnrealEngine \
  -f infra/deploy/unreal-cymonkey-fixture/Containerfile \
  -t jangolova/unreal-cymonkey-fixture:local .
```

The default entrypoint launches the packaged game with `-nullrhi` for
headless semantic tests. A display-capable runtime image may override
`UNREAL_FIXTURE_EXECUTABLE` and the entrypoint arguments when pixel transport
is part of a separate test.
