# Optional realtime adapter

This package implements `blockade.RealtimeSession` using the Realtime WebSocket
API dialect. `Open` requires a host-selected WebSocket URL and model ID; voice,
instructions, tools, credential resolver, and dialer are supplied explicitly.
It contains no built-in endpoint/model catalog or device integration.

The attachment sends one session configuration with manual turn detection.
`SendText` adds input; `SendAudio` appends mono PCM16 little-endian frames at
24 kHz. `CommitAudio` commits the input turn, `Respond` explicitly requests a
response, and `Interrupt` cancels generation. Capturing, framing, forwarding,
playing, and clearing already buffered output are the host's responsibility.
`Interrupt` does not truncate conversation audio or stop a device playback queue.

`Receive` returns normalized text/audio deltas and complete tool proposals, plus
raw events for other protocol messages. Receiving a tool call never invokes it.
The host can use Console to inspect and invoke a proposal, send its result with
`SendToolResult`, and choose whether to request a new response.

Read/write calls support contexts. Cancellation closes only this model socket
to unblock I/O; it never closes Board connections or terminates processes.
Use one receive loop, close the session yourself, and keep device channels within
their existing grants and byte/time limits. Audio frames are bounded at 4 MiB
per send, and incoming events at 16 MiB.

Protocol reference: [Realtime conversations](https://developers.openai.com/api/docs/guides/realtime-conversations).
