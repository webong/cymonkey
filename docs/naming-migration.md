# Cymonkey naming migration

Cymonkey is now the umbrella product name for this repository. The platform
contains two primary product areas:

```text
Cymonkey
├── Jangolova — direct interaction, presentation, and MCP tool server
└── Blockade  — read-only visual observation and inference
```

## Names in the current release

The repository is in a compatibility-first migration. These names remain
stable until a versioned replacement is available:

| Surface | Current name | Migration policy |
| --- | --- | --- |
| Product/platform | Cymonkey | Use Cymonkey for new product documentation and positioning. |
| Interaction/tool server | Jangolova | Keep as the runtime and executable identity during this migration. |
| Vision subsystem | Blockade | Keep as a first-class Cymonkey subsystem. |
| Semantic protocol | `jangolova.cymonkey/v1alpha2` | Do not rename in place; introduce a new protocol version if needed. |
| Environment variables | `JANGOLOVA_*` | Keep for compatibility; add `CYMONKEY_*` aliases before deprecating them. |
| Go/module and package paths | `jangolova/...` | Keep until a compatibility module and import migration exist. |
| MCP tool names | `jangolova_*` | Keep stable so existing agents do not lose access to tools. |

## Migration order

1. Establish Cymonkey as the umbrella brand in product documentation.
2. Add Cymonkey-facing executable, environment, and distribution aliases while
   retaining Jangolova names.
3. Update visible extension and distribution branding, with compatibility tests
   for existing manifests and integrations.
4. Only then consider new protocol, module, or package namespaces. Existing
   `jangolova.*` identifiers remain valid indefinitely or until an explicit
   versioned migration is published.

The central rule is that branding changes must not silently change ownership:
Jangolova executes approved interaction and presentation calls, while Blockade
observes pixels and returns normalized visual results. External agents still
own planning and decisions.
