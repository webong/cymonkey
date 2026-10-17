// display serves Board's display engine as an ext package that speaks the
// display.engine contract. The host resolves credentials and sends the prepared
// connection material with the target; the engine reaches cooperative helpers
// and runs its worker processes itself.
//
// Services that need the host's endpoint policy or credential handling (endpoint
// validation, worker environments for authenticated endpoints, authenticated
// WebSocket dialing, redaction and the Safari bridge) are not available yet and
// fail with a clear error. They move behind host callbacks once ext provides
// guest-to-host calls, so the policy code stays in one place.
package main

import (
	"context"
	"fmt"
	"os"

	"jangolova/displaykit/serve"
)

func main() {
	if err := serve.Run(context.Background(), newEngine()); err != nil {
		fmt.Fprintln(os.Stderr, "display:", err)
		os.Exit(1)
	}
}
