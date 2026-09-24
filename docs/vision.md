# Vision

Jangolova gives agents one semantic way to operate existing interfaces and
create dynamic interfaces, regardless of where those systems run.

> Given a caller-owned target, attach the appropriate interaction engine and
> expose what the agent can observe, do, and present.

The interaction engine may be Playwright, Puppeteer, a Three.js experience, a
Unity package, an Unreal plugin, or another integration. The target may be
Chromium, a native player, a physical machine, a VM, or an OCI workload.

Jangolova deliberately does not provision targets. Xallet can create the
browser, display, application, network, and credentials, then hand Jangolova a
private endpoint. Standalone users can supply the same endpoint directly.

The durable abstraction is semantic interaction—`capabilities`, `describe`,
`act`, and `events`—not a specific browser, display server, or container tool.

Operating includes semantic browser/scene actions and, when no richer contract
exists, display-level observation plus pointer and keyboard actions. Creating
includes dynamic web, Three.js, Godot, Unity, Unreal, Blender, and future presentation engines.
In both cases Jangolova owns interface behavior while the target provider owns
the runtime, display, and lifecycle.

See [Interface creation and operation](interface-model.md) for the complete
product model.
