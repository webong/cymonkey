# Jangolova library

`jangolova` is the one module that the host (`src`) and every guest plugin depend on
for Cymonkey's contracts. A package here is a contract plus the host role for
it, with no engine or product behind it, so a process can embed it to host
plugins whether that process is the host or a guest that hosts others.

| Package | Contract | Holds |
| --- | --- | --- |
| `jangolova/contentkit` | `content.provider` | the shared content contract: documents and schemas, validation, provider adapter types and errors, model manifest types. The engine that runs providers is `pkg/console/contentengine` |
| `jangolova/devicekit` | `device.provider` | device contract, `Registry` grant gate, `host`, provider builder `device`, `plugin/` ext adapter |
| `jangolova/generationkit` | generation | messages, streaming, structured output, embeddings, speech, transcription, model catalog |

Implementations live elsewhere (`pkg/board`, `pkg/content-*`, `pkg/board/display`)
and build against it. The module may depend on ext and on small libraries,
never on the host or on a guest; the dependency tests in each package enforce
that.
