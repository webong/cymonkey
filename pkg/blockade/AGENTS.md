# Blockade boundary

Blockade must remain provider-neutral. Keep only generic contracts, validation,
explicit registration, and lifecycle primitives in this module.

- Do not add vendor clients, vendor imports, provider switches, built-in provider
  names, endpoint defaults, built-in model catalogs, or model-name capability guesses.
- Provider IDs and model IDs are opaque caller-supplied strings. Registries and
  catalogs start empty and contain only explicitly registered bindings.
- Put API dialects, authentication headers, and provider-specific payloads in
  optional packages outside `pkg/blockade`. Hosts select and configure them.
- Generation returns proposed tool calls as data. Tool execution and subsequent
  turns belong to the embedding host or `pkg/console` caller.
- Never route to a fallback provider or execute a proposed tool implicitly.
