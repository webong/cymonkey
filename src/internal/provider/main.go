package provider

import (
	"errors"
	"fmt"
	"io"

	"cymonkey/src/internal/orchestrator"
)

// RegistryFactory supplies already composed display adapters to this private
// command implementation. Library-specific registration remains outside
// src/internal.
type RegistryFactory func() (*orchestrator.Registry, error)

func registryFrom(factory RegistryFactory) (*orchestrator.Registry, error) {
	if factory == nil {
		return nil, errors.New("display adapter registry factory is required")
	}
	return factory()
}

// Run executes Cymonkey's display command workflow. The root command owns the
// public invocation and delegates its implementation here.
func Run(invocation string, args []string, stderr io.Writer, registryFactory RegistryFactory) error {
	if len(args) == 0 {
		return fmt.Errorf("%s requires adapters, connect, serve, or serve-tools", invocation)
	}
	switch args[0] {
	case "adapters":
		return enginesCommand(args[1:], registryFactory)
	case "connect":
		return connectEngineCommand(args[1:], registryFactory)
	case "serve":
		return serveEngineProviderCommand(args[1:], registryFactory)
	case "serve-tools":
		return serveMCPCommand(args[1:], registryFactory)
	case "help", "-h", "--help":
		return usage(stderr, invocation)
	default:
		return fmt.Errorf("unknown display command %q", args[0])
	}
}

func usage(w io.Writer, invocation string) error {
	_, err := fmt.Fprintf(w, `Usage: %s <command>

Commands:
  adapters                Discover display adapters and availability
  connect                 Attach one adapter to a caller-owned target
  serve                   Serve the authenticated display interaction API
  serve-tools             Serve display tools over stdio or HTTP`, invocation)
	return err
}
