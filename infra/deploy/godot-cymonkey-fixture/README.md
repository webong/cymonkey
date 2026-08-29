# Godot Cymonkey fixture image

This image is the first license-free Cymonkey render runtime environment. It uses a
Godot 4 Linux image, copies `pkg/godot-cymonkey` into the fixture project, and
starts Godot headless. The runtime has no display or GPU requirement.

Build locally or in CI:

```sh
docker build \
  --build-arg GODOT_IMAGE=barichello/godot-ci:4.3 \
  -f deploy/godot-cymonkey-fixture/Containerfile \
  -t jangolova/godot-cymonkey-fixture:local .
```

For production or reproducible CI, mirror and pin the Godot image by digest in
your private registry. Cymonkey tokens should be injected at runtime, never put
in the image or project files.
