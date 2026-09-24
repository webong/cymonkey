# Cymonkey naming migration

Cymonkey is now the operator and host for this repository's independently
executable subsystems. It contains two primary product areas:

```text
Cymonkey
├── host/operator — entry, extension, policy, composition, and coordination
├── Jangolova    — standalone display interfaces and MCP tool server
└── Blockade     — standalone local and cloud inference interfaces
```

## Names in the current release

Cymonkey is the repository and module identity. The subsystem and public-wire
names below remain stable where they describe Jangolova or a compatibility
surface:

| Surface | Current name | Migration policy |
| --- | --- | --- |
| Operator/host | Cymonkey | Use `cymonkey` for composition and lifecycle management. |
| Interaction/tool server | Jangolova | Keep as a standalone runtime and executable. |
| Inference subsystem | Blockade | Keep as a standalone runtime and executable. |
| Cymonkey protocol | `cymonkey/v1alpha1` | Versioned Cymonkey contract; do not rename in place. |
| Environment variables | `JANGOLOVA_*` | Keep for compatibility; add `CYMONKEY_*` aliases before deprecating them. |
| Go/module and package paths | `cymonkey/...` | Repository-owned Go imports use the Cymonkey module path. |
| MCP tool names | `jangolova_*` | Keep stable so existing agents do not lose access to tools. |

## Migration order

1. Establish Cymonkey as the repository, module, and host/operator identity.
2. Retain standalone Jangolova and Blockade commands and their runtime-library
   identities.
3. Add host-level operator APIs and health/discovery without moving subsystem
   ownership into the host.
4. Update visible distribution branding, with compatibility tests for existing
   manifests and integrations.
5. Keep existing Jangolova public-wire names valid until an explicit versioned
   replacement is published.

The central rule is that host composition must not silently change ownership:
Jangolova executes approved display interaction and presentation calls, while
Blockade runs inference through local or cloud backends. Its current public
request uses images; sound remains to be defined. Cymonkey coordinates and
extends these modules; external agents still own planning and decisions.
