// Package service defines the content.service contract: the host-facing surface
// of the content engine when it runs as an ext package. The host sends a public
// content command and receives what the command wrote; the engine hosts the
// content providers itself.
package service

import plugin "github.com/webong/ext/pkg/plugin-go/contract"

const (
	ContractName    = "content.service"
	ContractVersion = "v1"

	// MaxOutputBytes bounds what one command may return on each stream.
	MaxOutputBytes = 16 << 20
)

// Operations lists every method a content.service package serves.
var Operations = []string{"command"}

// Contract is the content.service contract a package declares. The command
// operation reports progress: a command's log lines stream to the host while it
// runs.
func Contract() plugin.Contract {
	return plugin.Contract{
		ContractRef: plugin.ContractRef{Name: ContractName, Version: ContractVersion},
		Operations:  []plugin.Operation{{Name: "command", Progress: true}},
	}
}

// CommandRequest runs one `cymy content` command. Args are the arguments after
// "content".
type CommandRequest struct {
	Args []string `json:"args"`
}

// CommandResult is what the command wrote and, if it failed, why. A failed
// command still returns its output, so the host can show what was printed
// before the failure.
type CommandResult struct {
	Stdout []byte `json:"stdout,omitempty"`
	Stderr []byte `json:"stderr,omitempty"`
	Error  string `json:"error,omitempty"`
	// Truncated reports that an output stream exceeded MaxOutputBytes.
	Truncated bool `json:"truncated,omitempty"`
}
