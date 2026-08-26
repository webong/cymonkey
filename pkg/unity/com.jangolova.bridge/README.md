# Jangolova Bridge for Unity

This package lets a caller-owned Unity player connect outward to Jangolova's
authenticated loopback WebSocket bridge. It implements
`jangolova.bridge/v1alpha1` and exposes a small, agent-operable scene surface.

## Install

In Unity 2022.3 or newer, use **Window > Package Manager > Add package from
disk** and choose this package's `package.json`. For a repository-relative
project dependency, add:

```json
{
  "dependencies": {
    "com.jangolova.bridge": "file:../../integrations/unity/com.jangolova.bridge"
  }
}
```

Add `JangolovaSceneBridge` to one GameObject, or import the **Basic Dynamic
Scene** sample. A normal editor or player run without bridge connection values
does nothing. Xallet or the native launcher injects values supplied for the
Jangolova interaction session when it starts the player.

## Capabilities

- `scene.describe`
- `object.create`
- `object.update`
- `object.remove`
- `camera.update`

The built-in surface is deliberately small. A project can implement
`IJangolovaBridgeHandler` and give it to `JangolovaBridgeClient` to expose its
own domain-specific capabilities without adopting the built-in scene model.
When Unity's legacy input manager is enabled, clicking a tracked object also
emits an `object.selected` cursor event. Projects using only the newer Input
System can publish their own domain events through a custom handler.

The package accepts only `ws://` loopback endpoints and sends the injected token
as an Authorization bearer header. Tokens are never placed in URLs or logs.

## Tests

To expose the included tests in Unity's Test Runner, add the package name to the
consuming project's `Packages/manifest.json`:

```json
{
  "testables": ["com.jangolova.bridge"]
}
```

Run both Edit Mode and Play Mode tests before producing a desktop player.
