// gateway serves the display byte gateway as an ext package that speaks the
// gateway.service contract. The host forwards `cymy device gateway serve`; the
// gateway runs it and its log lines stream back while it serves.
package main

import (
	"context"
	"io"

	"board/gatewaycmd"
	"jangolova/commandkit/serve"
	"jangolova/gatewaykit/service"
)

func main() {
	serve.Main(service.ContractName, service.ContractVersion,
		func(ctx context.Context, args []string, _ io.Reader, _, stderr io.Writer) error {
			return gatewaycmd.Run(ctx, args, stderr)
		})
}
