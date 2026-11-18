# Device kit

The device kit is the host role for the `device.provider` contract: what a
process needs to discover devices, open a host-approved session and invoke
bounded capabilities. The host (`src`) embeds it, and so can a guest that hosts
other plugins.

It holds:

- the contract: `Device`, `Grant`, `Action`, `Result`, capabilities, drive and
  stream types, and events;
- `Registry`, the grant gate. It lists devices and refuses any action whose
  capability or resource the grant does not allow;
- `host`, for registering providers and approving grants;
- `device`, a builder for providers from device-bound operations;
- `plugin`, which opens an installed ext package that serves `device.provider`
  and enforces the admission policy for each session.

It contains no device implementation. Desktop, audio, storage, WebRTC, VNC
gateway and keyboard providers live in `pkg/board` and build against the kit.
