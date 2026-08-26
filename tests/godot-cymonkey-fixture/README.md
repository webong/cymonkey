# Godot Pacman headless fixture

This Godot 4 project is the license-free reference fixture for
`pkg/godot-pacman`. The container copies the package into the project, launches
the scene with `--headless`, and exposes the explicitly registered
The fixture draws a small house from Godot primitives and exposes its
explicitly registered semantic resources through an authenticated `pacman-ws`
listener: the house, door, windows, visitor, interior light, camera, and
status label. The container is long-lived by default; inject
`JANGOLOVA_PACMAN_TOKEN` and bind port `8090` only to a private interface or
SSH tunnel.

The rendered fixture accepts `JANGOLOVA_CAPTURE_PATH` and saves a PNG after
startup and after each resource-change event. This makes a Pacman action
observable as both a semantic event and a changed screen frame.

The fixture is intentionally source-only. It can run in CI without a display,
GPU, proprietary editor license, or engine installation on the developer's
machine.
