# Kit

`kit` is the one module that the host (`src`) and every guest plugin depend on
for Cymonkey's contracts. A package here is a contract plus the host role for
it, with no engine or product behind it, so a process can embed it to host
plugins whether that process is the host or a guest that hosts others.

| Package | Contract | Holds |
| --- | --- | --- |
| `kit/contentkit` | `content.provider` | content documents and schemas, validation, adapter registry and engines, model and runtime manifests, HTTP service and client, `plugin/` ext adapter |
| `kit/devicekit` | `device.provider` | device contract, `Registry` grant gate, `host`, provider builder `device`, `plugin/` ext adapter |
| `kit/generationkit` | generation | messages, streaming, structured output, embeddings, speech, transcription, model catalog |

Implementations live elsewhere (`pkg/board`, `pkg/content-*`, `pkg/jangolova`)
and build against `kit`. The module may depend on ext and on small libraries,
never on the host or on a guest; the dependency tests in each package enforce
that.
