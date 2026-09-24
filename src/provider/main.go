package provider

import (
	"fmt"
	"io"
)

// Run executes the standalone provider CLI workflow. The root command owns
// process setup and delegates the provider implementation here so the command
// path stays outside the source tree.
func Run(args []string, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("cymonkey provider requires engines, connect-engine, serve-engine-provider, or serve-mcp")
	}
	switch args[0] {
	case "engines":
		return enginesCommand(args[1:])
	case "connect-engine":
		return connectEngineCommand(args[1:])
	case "serve-engine-provider":
		return serveEngineProviderCommand(args[1:])
	case "serve-mcp":
		return serveMCPCommand(args[1:])
	case "help", "-h", "--help":
		return usage(stderr)
	default:
		return fmt.Errorf("unknown provider command %q", args[0])
	}
}

func usage(w io.Writer) error {
	_, err := fmt.Fprintln(w, `Usage: cymonkey provider <command>

Commands:
  engines                 Discover interaction-engine adapters and availability
  connect-engine          Attach one engine to a caller-owned target
  serve-engine-provider   Serve the authenticated interaction-engine API
  serve-mcp               Serve Jangolova's direct MCP tool interface (stdio or HTTP)`)
	return err
}
