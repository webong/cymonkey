# Explicit tool handoff

Run from `pkg/console`:

```sh
go run ./examples/handoff
go run ./examples/handoff -action invoke -key board
go run ./examples/handoff -action reject
go run ./examples/handoff -action direct -key jangolova
```

The example registers an in-memory note lookup tool and, for the model modes,
a local scripted generator. It uses no credentials, network services, files,
or device attachments. The generator is example code; it is not a real model.

| Action | Model requests | Tool invocations | Output |
| --- | --- | --- | --- |
| `inspect` (default) | 1 | 0 | Tool definitions and a proposal |
| `invoke` | 1 | 1 | Selected tool result and continuation transcript |
| `reject` | 1 | 0 | Public failure result and continuation transcript |
| `direct` | 0 | 1 | Direct invocation result |

`Continue` prepares a transcript. There is no second model request. All output
is JSON on stdout, so a host can inspect the same data from a CLI or a UI.

In a real application, supply your configured `blockade.Generator`, register
only tools bound to approved resources, and make the invocation decision in
your own host. Replace the note handler with application code or a Console
SDK binding. Keep provider endpoint and credential configuration outside
Blockade and Console.
