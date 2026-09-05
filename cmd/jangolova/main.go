package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "engines":
		err = enginesCommand(os.Args[2:])
	case "connect-engine":
		err = connectEngineCommand(os.Args[2:])
	case "serve-engine-provider":
		err = serveEngineProviderCommand(os.Args[2:])
	case "serve-mcp":
		err = serveMCPCommand(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "jangolova: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "jangolova: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: jangolova <command>

Commands:
  engines                 Discover interaction-engine adapters and availability
  connect-engine          Attach one engine to a caller-owned target
  serve-engine-provider   Serve the authenticated interaction-engine API
  serve-mcp               Serve Jangolova's direct MCP tool interface (stdio or HTTP)`)
}
