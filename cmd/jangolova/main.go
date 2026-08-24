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
	case "models":
		err = modelsCommand(os.Args[2:])
	case "connect-model":
		err = connectModelCommand(os.Args[2:])
	case "blockade":
		err = blockadeCommand(os.Args[2:])
	case "serve-engine-provider":
		err = serveEngineProviderCommand(os.Args[2:])
	case "serve-grimlock":
		err = serveGrimlockCommand(os.Args[2:])
	case "serve-grimlock-mcp":
		err = serveGrimlockMCPCommand(os.Args[2:])
	case "serve-grimlock-acp":
		err = serveGrimlockACPCommand(os.Args[2:])
	case "gl":
		err = grimlockCommand(os.Args[2:])
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
  models                  Discover registered Grimlock model connector protocols
  connect-model           Test connection to a caller-supplied model provider
  blockade                Validate Blockade config or run one observation
  serve-engine-provider   Serve the authenticated interaction-engine API
  serve-grimlock           Serve the authenticated Grimlock agent API
  serve-grimlock-mcp       Serve Grimlock through MCP (stdio or HTTP)
  serve-grimlock-acp       Serve Grimlock through ACP stdio
  gl                       Serve Grimlock (use --mcp or --acp for a protocol adapter)`)
}
