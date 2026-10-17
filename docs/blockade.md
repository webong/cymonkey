# Blockade implementation notes

Blockade is the library Cymonkey currently uses to implement content
observation and interpretation. It is not a Cymonkey product surface and it
does not provide a CLI.

Normal Cymonkey configuration, commands, services, manifests, and plugins are
documented in [Content observation and interpretation](content.md). They use
the `cymonkey.content`, `cymonkey.observation`, `cymonkey.model`, and
`cymonkey.runtime` contracts.

At the composition boundary Cymonkey converts those public values to
Blockade's native Go types. Blockade then validates requests and results,
selects an adapter, manages model and runtime artifacts, and supervises source
adapter processes. Responses are converted back before they cross the public
boundary.

Developers embedding Blockade directly can use its native contracts without
depending on Cymonkey. See the [content kit README](../lib/jangolova/contentkit/README.md).
