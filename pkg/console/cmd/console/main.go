// console serves the console (tools, model bindings, MCP and the HTTP API) as an
// ext package that speaks the console.service contract. The host sends a
// `cymy console` command; the console runs it. Started with arguments instead of
// by a host it runs the command directly with the process's own input and
// output, which is how the host runs the commands that speak a stdio protocol
// (the MCP server and the live event stream).
package main

import (
	"context"
	"io"

	"console/consolecmd"
	"jangolova/commandkit/serve"
	"jangolova/consolekit/service"
)

func main() {
	serve.Main(service.ContractName, service.ContractVersion,
		func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
			return consolecmd.Run(ctx, args, stdin, stdout, stderr)
		})
}
