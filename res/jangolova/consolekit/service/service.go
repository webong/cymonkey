// Package service defines the console.service contract: the host-facing surface
// of the console when it runs as an ext package. It is a command service (see
// jangolova/commandkit): the host sends a `cymy console` command and gets back
// what it wrote, with its log lines streamed as progress.
package service

import (
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"jangolova/commandkit"
)

const (
	ContractName    = "console.service"
	ContractVersion = "v1"
)

// Operations lists every method a console.service package serves.
var Operations = commandkit.Operations

// Contract is the console.service contract a package declares.
func Contract() plugin.Contract { return commandkit.Contract(ContractName, ContractVersion) }
