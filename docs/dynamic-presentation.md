# Dynamic Presentation

Jangolova owns presentation integrations as well as imperative interaction.
Created presentations are not passive output: they expose semantic state,
actions, and events so agents can subsequently operate the interfaces they
created.

## Web and Three.js

The repository's Three.js experience demonstrates semantic scene description,
object and camera actions, pointer events, and dynamic rendering. A browser
target is still supplied by Xallet or the native system. Jangolova uses its
Playwright/Puppeteer interaction engine to navigate and operate that target;
it does not launch the browser.

The `web-presentation` adapter now exposes a provider-visible declarative web
surface. It attaches to a caller-owned CDP browser, optionally navigates to
`engine.source`, and forwards `presentation.create`, `presentation.replace`,
`presentation.patch`, `presentation.write`, `presentation.execute`,
`presentation.describe`, `presentation.activate`, `presentation.capture`, and
the cursor-based event stream. The browser still
belongs to Xallet or the native host; the adapter only connects to it.

The minimal host in `etc/web-presentation` is a reference renderer. It can
be served by any static server and then attached through the provider API.

## Unity and Unreal

The Unity package implements the common cooperative bridge in a caller-owned
player. Jangolova owns the package and semantic operations; Xallet or the
native system owns the player process, display, and placement. Unreal should
follow the same boundary with an engine-specific plugin.

Presentation capabilities are descriptive, not authorization. The calling
agent system decides which write or externally-effectful actions are allowed.

Presentation artifacts and behavior belong to Jangolova. Serving, player
launch, graphics/display allocation, and process lifecycle belong to Xallet or
another target provider.

See [Web presentation provider handoff](presentation-provider.md) for the
target contract and the responsibilities Xallet should implement.
