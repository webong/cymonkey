# Console boundary

Console is the composition library for humans and agents, under `pkg/console`.
Keep control with the embedding caller.

- Receiving a model proposal, discovering an MCP tool, or copying a transcript
  never invokes a tool or requests another model turn.
- Each explicit invocation executes one selected tool. Each explicit model
  request performs one turn. Do not add an autonomous loop, planner, memory
  manager, automatic retry, or background tool execution.
- SDK bindings borrow caller-owned connections and retain their selected
  resources and capabilities. They never open devices or expand grants.
- Provider protocols, endpoint defaults, built-in model catalogs, and credential discovery
  belong to external adapters. Console depends on neutral library contracts.
- Audio consumers and other destinations are selected by the host. Close only
  attachments owned by the binding; preserve borrowed connection lifetimes.
- Examples must make invocation decisions explicit and distinguish scripted
  local demonstrations from actual model integrations.
