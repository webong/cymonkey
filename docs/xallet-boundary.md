# Xallet Boundary

Jangolova owns interaction and presentation engines. Board owns the
device I/O implementations: native display actions and probes, virtual display
and audio runtimes, volume inspection/archive transfer, endpoint health, and
the authenticated WebSocket gateway for VNC, RDP, and SPICE streams.
Xallet owns deployment and supervision of target and device runtimes.

Jangolova retains Playwright, Puppeteer, Three.js, Unity and Unreal plugins,
their semantic interaction protocol, and adapter-local workers. Xallet owns
Chromium/WebKit/Gecko processes, browser-driver processes, native applications,
live surface instances, CDP/BiDi/WebDriver exposure, OCI placement, networking,
secrets, policy, and target-session lifecycle. Board supplies reusable device
mechanics and display/audio runtime implementations for that host to deploy.

Board's gateway relays bytes to a caller-selected private display endpoint;
it does not create a display, choose an endpoint, or manage a target session.
Xallet may deploy the Board gateway alongside a target and pass the resulting
private WebSocket endpoint to Jangolova.

Board's SDKs and runtime recipes are now maintained in Cymonkey. Xallet's
existing copies remain until its later integration replaces them with Board
dependencies and artifacts. The [extraction inventory](device-sdk-extraction.md)
documents the source mapping, APIs, configuration changes, and adoption steps.

For Safari MCP, Xallet also owns `safaridriver --mcp` and the private
stdio-to-Streamable-HTTP relay. Jangolova owns only the MCP client and semantic
capability mapping.

The integration is target-in:

1. Xallet creates and starts a target.
2. Xallet resolves a private endpoint or native handle.
3. Xallet asks Jangolova to connect an interaction engine to that target.
4. Agents call Jangolova's capabilities through the authenticated provider.
5. Disconnecting Jangolova does not terminate the Xallet-owned target.

The same sequence works without Xallet when a native system supplies the
target coordinates directly.

The exact browser target shapes, ownership invariants, cleanup sequence, and
Xallet implementation checklist are in
[Browser target protocols](browser-target-protocols.md).
