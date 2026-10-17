// Package service defines the content.service contract: the host-facing surface
// of the content engine when it runs as an ext package. The host sends a public
// content command and receives what the command wrote; the engine hosts the
// content providers itself.
package service

const (
	ContractName    = "content.service"
	ContractVersion = "v1"

	// MaxOutputBytes bounds what one command may return on each stream.
	MaxOutputBytes = 16 << 20
)

// Operations lists every method a content.service package serves.
var Operations = []string{"command"}

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
