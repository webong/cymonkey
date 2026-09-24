# Runtime matrix

| Engine | Runtime | Distribution | Constraint |
| --- | --- | --- | --- |
| Godot 4 | `infra/deploy/godot-cymonkey-gpu` | Open-source; build from a Godot CI base | License-free reference runtime |
| Unreal 5.8 | `ghcr.io/webong/jangolova/unreal-cymonkey-gpu:5.8` | Public GHCR packaged fixture | Custom projects need approved Unreal build/runtime bases |
| Unity 2022.3 | `infra/deploy/unity-cymonkey-gpu` | Operator-supplied private licensed Editor image | License activation stays in runtime secrets |

All images target Linux `amd64`, expose TCP `8090`, and support an Xvfb fallback. Use `--gpus all` only with a compatible NVIDIA runtime; Xvfb alone is off-screen display emulation.

For Unreal plugin integration, download `JangolovaPacman-0.1.0-UE5.8` from the public GitHub Release and copy the extracted plugin directory to `MyProject/Plugins/JangolovaPacman`.
