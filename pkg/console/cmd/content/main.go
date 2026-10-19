// content serves the content engine as an ext package that speaks the
// content.service contract. The host sends a public content command; the engine
// runs it, hosting the content providers it needs. What the command prints to
// stdout is returned with the call; what it logs to stderr streams to the host as
// progress events while it runs, which is how long-running commands such as
// serve report.
package main

import (
	"context"
	"io"

	"console/contentcmd"
	"jangolova/commandkit/serve"
	"jangolova/contentkit/service"
)

func main() {
	serve.Main(service.ContractName, service.ContractVersion,
		func(ctx context.Context, args []string, _ io.Reader, stdout, stderr io.Writer) error {
			return contentcmd.RunContext(ctx, args, stdout, stderr)
		})
}
