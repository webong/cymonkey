# Cymonkey public naming

Cymonkey is the product, host, operator API, and command line identity. Its
public domains are:

```text
Cymonkey
├── display  — interaction and presentation
├── content  — observation and multimodal interpretation
└── device   — input and output under explicit grants
```

## Stable public names

| Surface | Name |
| --- | --- |
| CLI | `cymy` |
| Plugin manifest | `cymonkey.plugin/v1alpha1` |
| Plugin contracts | `cymonkey.display`, `cymonkey.content`, `cymonkey.device` |
| MCP tools | `cymonkey_display_*` |
| Content configuration | `cymonkey.content-config/v1alpha1` |
| Device wire contract | `cymonkey.device/v1alpha1` |
| Service environment | `CYMONKEY_DISPLAY_*`, `CYMONKEY_CONTENT_*`, `CYMONKEY_DEVICE_*` |

Internal library names and native contracts stay available to developers who
embed those libraries directly. They are not aliases in Cymonkey's public
protocol and do not appear in new Cymy commands, plugin packages, service
payloads, or operator routes.
