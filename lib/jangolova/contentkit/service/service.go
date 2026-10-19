// Package service defines the content.service contract: the host-facing surface
// of the content engine when it runs as an ext package. It is a command service
// (see jangolova/commandkit): the host sends a public content command and gets
// back what it wrote, with its log lines streamed as progress.
package service

import (
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"jangolova/commandkit"
)

const (
	ContractName    = "content.service"
	ContractVersion = "v1"

	// MaxOutputBytes bounds what one command may return on each stream.
	MaxOutputBytes = commandkit.MaxOutputBytes
)

// Operations lists every method a content.service package serves.
var Operations = commandkit.Operations

// Contract is the content.service contract a package declares.
func Contract() plugin.Contract { return commandkit.Contract(ContractName, ContractVersion) }

type (
	CommandRequest = commandkit.Request
	CommandResult  = commandkit.Result
)
