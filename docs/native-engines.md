# Native Interaction Engines

Jangolova does not launch native targets. Xallet or the native system launches
Unity, Unreal, or another application and supplies an interaction endpoint or
opaque handle.

## Pacman

New Unity and Unreal integrations use [Pacman](pacman.md), a shared semantic
presentation protocol over a caller-owned `pacman-ws` endpoint. The Jangolova
adapter dials that endpoint from the generic target descriptor. The application
owns the listener, renderer, display, and lifecycle. The Unity MVP is at
`pkg/unity-cymonkey`; the Unreal C++ implementation starts at
`pkg/unreal-cymonkey`. Both use the same transport-neutral resource and method
contract.

## Legacy cooperative bridge

The current native integration uses an authenticated loopback WebSocket and
the `jangolova.bridge/v1alpha1` methods. Jangolova owns the bridge host,
protocol, conformance validation, and Unity package. The target owner injects
the endpoint and short-lived token when it starts the application.

Passing a handle or endpoint never transfers target ownership. Disconnecting
Jangolova closes only its bridge connection and host; process and display
lifecycle remain with the target owner.

New work should use Pacman; the legacy bridge remains for compatibility.
