# Cymonkey public naming

Cymonkey is the product, host, operator API, and command line identity. Its
public domains are:

```text
Cymonkey
├── display  — interaction and presentation
├── content  — observation and multimodal interpretation
└── device   — input and output under explicit grants
```

## Product names

| Surface | Name |
| --- | --- |
| CLI | `cymy` |
| MCP tools | `cymonkey_display_*` |
| Content configuration | `cymonkey.content-config/v1alpha1` |
| Service environment | `CYMONKEY_DISPLAY_*`, `CYMONKEY_CONTENT_*`, `CYMONKEY_DEVICE_*` |

## Plugin wire names

Plugins are ext packages. Their contract and document names are generic and
carry no product or library name:

| Surface | Name |
| --- | --- |
| Plugin contracts | `display.engine@v1`, `content.provider@v1`, `device.provider@v1` |
| Methods | `<contract>.<operation>`, for example `content.provider.observe` |
| Content documents | `content.observation/v1`, `content.item/v1`, `content.model/v1`, `content.adapter/v1`, `content.runtime/v1`, `content.source/v1` |

Cymonkey owns these names and their payload schemas. ext owns only the
mechanism (name validation, method resolution) and lists the names in its
contract registry. The earlier `cymonkey.*` and `blockade.*` names are gone. Plugins do not
accept them; only already installed model and runtime manifests with the old
`apiVersion` still load, for one release.

`content.observation/v1` covers both the observation request and its result.
Display and device document versions (`cymonkey.display/v1alpha1` and similar)
and the display-engine runtime protocol `cymonkey/v1alpha1` are not plugin
contracts and keep their names for now.
