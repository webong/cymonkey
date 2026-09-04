# Blender Cymonkey fixture image

This is the headless CPU fixture for Jangolova's native `render/blender`
module. Blender is open source, so no engine license or proprietary base image
is required. The image accepts an operator-selected Blender base image:

```sh
docker build \
  --build-arg BLENDER_IMAGE=your-registry/blender:4.x \
  -f infra/deploy/blender-cymonkey-fixture/Containerfile \
  -t cymonkey/blender-fixture:local .
```

Run the source-only conformance fixture:

```sh
docker run --rm \
  -e JANGOLOVA_CYMONKEY_TOKEN=replace-at-runtime \
  cymonkey/blender-fixture:local
```

For a live Blender target, provide a bootstrap script that imports
`pkg/blender/blender_cymonkey.py`, registers caller-selected resources, creates
`CymonkeyWebSocketHost`, and registers its `poll` method with
`bpy.app.timers`. The token must be injected at runtime and never placed in
the image, scene, or repository.
