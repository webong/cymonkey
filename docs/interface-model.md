# Interface Creation and Operation

Jangolova exists to let an agent both operate interfaces that already exist and
create interfaces that can be presented dynamically. Browser automation is one
implementation of this goal, not the product boundary.

## The two responsibilities

```text
                         Jangolova
                             |
              +--------------+--------------+
              |                             |
       Operate interfaces             Create interfaces
              |                             |
  observe, click, type, scroll     compose, render, update, animate
  inspect, navigate, evaluate      expose semantic controls and events
```

### Operate existing interfaces

Jangolova interaction engines should be able to:

- describe the current interface and its actionable elements;
- click, select, type, scroll, drag, hover, and press keys;
- navigate and evaluate code where the target supports it;
- observe screenshots, accessibility information, DOM or scene structure,
  console output, network activity, and interaction events;
- use semantic actions when available and display-level pointer/keyboard input
  when the target exposes only a visual surface.

Current browser adapters provide semantic interaction through CDP, WebDriver
BiDi, WebDriver Classic, and Safari MCP. Cooperative Three.js and Unity bridges
expose application-defined scene actions. General display-level observation and
pointer/keyboard input remain a planned adapter family.

### Create dynamic interfaces

Jangolova presentation engines should be able to:

- create web-based 2D interfaces and Three.js 3D scenes;
- create and update Unity and Unreal experiences through engine plugins;
- present dashboards, controls, simulations, visual explanations, and
  agent-generated views;
- change interface structure and content while it is running;
- expose the created interface through the same semantic `describe`, `act`, and
  `events` vocabulary used to operate existing interfaces.

Jangolova owns the presentation definitions, interaction behavior, scene
schemas, reusable assets, and engine integrations. A target provider still owns
serving or launching the corresponding web application, player, browser, or
display runtime.

## Interaction levels

Jangolova selects the richest interaction level offered by a target:

1. **Cooperative semantic interface** — a Three.js, Unity, Unreal, or native
   integration declares objects, actions, events, and state explicitly.
2. **Runtime automation protocol** — Playwright, Puppeteer, WebDriver, or MCP
   inspects and operates browser content through a typed control endpoint.
3. **Display-level interaction** — Jangolova observes frames and requests
   pointer or keyboard actions through a target-provider surface/input contract
   when no richer semantic protocol exists.

These levels can coexist. A semantic scene description may be paired with a
screenshot, and a runtime-specific action can fall back to a coordinate-based
pointer action when policy permits.

## Display-level contract direction

The target provider—Xallet, a native host, VM manager, or another system—owns
the actual display and input injection mechanism. This can be backed by a
window system, VNC, WebRTC, an operating-system accessibility service, or a
platform-specific API.

Jangolova consumes a provider-neutral target contract containing capabilities
and connection coordinates. The planned semantic surface vocabulary includes:

- `display.describe` and `display.capture`;
- `pointer.move`, `pointer.click`, `pointer.drag`, and `pointer.scroll`;
- `keyboard.type`, `keyboard.press`, and key combinations;
- viewport, coordinate-space, focus, and input-policy metadata;
- frame, focus, resize, and input-result events.

Jangolova does not create the display, attach physical devices, expose raw VNC,
or decide operating-system permissions. It translates agent intent into the
available target interaction contract.

## Runtime ownership

```text
External agent or application
       |
       | intent and policy-approved actions
       v
Jangolova
       |-- interaction clients and semantic translation
       |-- presentation definitions and engine integrations
       v
Caller-owned target contract
       |
       v
Xallet, native host, container, VM, or physical machine
       |-- browser/application/player process
       |-- display/window/surface and input mechanism
       |-- networking, credentials, placement, and lifecycle
```

Xallet is optional. A native host can provide the same endpoints and handles,
as demonstrated by direct Jangolova attachment to native Chrome and Safari.

## Product invariant

Jangolova owns **how an interface is understood, operated, and composed**.
The target provider owns **where the interface runs, how its pixels are
displayed, and the lifecycle of the underlying runtime**.

Disconnecting Jangolova must release its interaction connection without
terminating the browser, application, player, display, or caller-owned target
session.
