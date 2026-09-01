# Cymonkey naming migration

Cymonkey is now the operator and host for this repository's independently
executable subsystems. It contains two primary product areas:

```text
Cymonkey
├── host/operator — lifecycle, composition, and coordination
├── Jangolova    — standalone interaction, presentation, and MCP tool server
└── Blockade     — standalone read-only visual observation and inference
```

## Names in the current release

The repository is in a compatibility-first migration. These names remain
stable until a versioned replacement is available:

| Surface | Current name | Migration policy |
| --- | --- | --- |
| Operator/host | Cymonkey | Use `cymonkey` for composition and lifecycle management. |
| Interaction/tool server | Jangolova | Keep as a standalone runtime and executable. |
| Vision subsystem | Blockade | Keep as a standalone runtime and executable. |
| Semantic protocol | `jangolova.cymonkey/v1alpha2` | Do not rename in place; introduce a new protocol version if needed. |
| Environment variables | `JANGOLOVA_*` | Keep for compatibility; add `CYMONKEY_*` aliases before deprecating them. |
| Go/module and package paths | `jangolova/...` | Keep until a compatibility module and import migration exist. |
| MCP tool names | `jangolova_*` | Keep stable so existing agents do not lose access to tools. |

## Migration order

1. Establish Cymonkey as the host/operator in product documentation.
2. Add the `cymonkey` host executable and manifest while retaining standalone
   Jangolova and Blockade commands.
3. Add host-level operator APIs and health/discovery without moving subsystem
   ownership into the host.
4. Update visible distribution branding, with compatibility tests for existing
   manifests and integrations.
5. Only then consider new protocol, module, or package namespaces. Existing
   `jangolova.*` identifiers remain valid indefinitely or until an explicit
   versioned migration is published.

The central rule is that host composition must not silently change ownership:
Jangolova executes approved interaction and presentation calls, while Blockade
observes pixels and returns normalized visual results. Cymonkey coordinates
their processes; external agents still own planning and decisions.
