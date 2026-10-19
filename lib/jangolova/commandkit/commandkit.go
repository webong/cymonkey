// Package commandkit is the shared shape of a service that runs `cymy`
// commands: the host forwards a command to an ext package, the package runs it,
// and what the command prints comes back. The content and console services use
// it. A command's stdout is its result and returns whole with the call; its
// stderr streams to the host as progress events while it runs.
package commandkit

import plugin "github.com/webong/ext/pkg/plugin-go/contract"

const (
	// MaxOutputBytes bounds what one command may return on each stream.
	MaxOutputBytes = 16 << 20
	// MaxStdinBytes bounds the input a command may be given.
	MaxStdinBytes = 1 << 20
	// Operation is the one operation such a service serves.
	Operation = "command"
)

// Operations lists every method a command service serves.
var Operations = []string{Operation}

// Contract is the contract a command service package declares under name and
// version. Its command operation reports progress.
func Contract(name, version string) plugin.Contract {
	return plugin.Contract{
		ContractRef: plugin.ContractRef{Name: name, Version: version},
		Operations:  []plugin.Operation{{Name: Operation, Progress: true}},
	}
}

// Request runs one command. Args are the arguments after the command's own name.
type Request struct {
	Args []string `json:"args"`
	// Stdin is the command's input, for commands that read a request from it.
	Stdin []byte `json:"stdin,omitempty"`
}

// Result is what the command wrote and, if it failed, why. A failed command
// still returns its output, so the host can show what it printed first.
type Result struct {
	Stdout []byte `json:"stdout,omitempty"`
	// Stderr holds log text that was not delivered as progress events.
	Stderr []byte `json:"stderr,omitempty"`
	Error  string `json:"error,omitempty"`
	// Truncated reports that an output stream exceeded MaxOutputBytes.
	Truncated bool `json:"truncated,omitempty"`
}
