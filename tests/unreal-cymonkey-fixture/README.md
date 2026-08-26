# Unreal Cymonkey fixture environment

This is a separate caller-owned Unreal project for compiling and exercising
`pkg/unreal-pacman`. It is not part of the Jangolova provider image and it owns
its own Unreal process, World, renderer, and lifecycle.

The project discovers the plugin through `AdditionalPluginDirectories` and
starts `ACymonkeyFixtureGameMode`, which spawns one actor and registers the stable
resource `object:fixture` with the actions
`resource.describe` and `object.visibility.set`. When
`JANGOLOVA_PACMAN_TOKEN` is set, the fixture starts the package's UE 5.8
WebSocketServer listener on port 8090 (or `-PacmanPort=<port>`).

With an installed Unreal Engine 5.3 toolchain, generate project files, compile,
and run the automation suite using the normal Unreal commands for the host
platform. The project is intentionally source-only in this repository; cooked
assets and packaged binaries belong in the separate build output.

The optional container definition at
`deploy/unreal-pacman-fixture/Containerfile` uses operator-supplied Unreal build
and runtime images. It never assumes a public Unreal image or puts the Unreal
Editor in the Jangolova runtime image.
