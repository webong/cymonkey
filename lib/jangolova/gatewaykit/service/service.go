// Package service defines the gateway.service contract: the display byte
// gateway that `cymy device gateway` serves when it runs as an ext package. It
// is a command service (see jangolova/commandkit).
package service

import (
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"jangolova/commandkit"
)

const (
	ContractName    = "gateway.service"
	ContractVersion = "v1"
)

// Operations lists every method a gateway.service package serves.
var Operations = commandkit.Operations

// Contract is the gateway.service contract a package declares.
func Contract() plugin.Contract { return commandkit.Contract(ContractName, ContractVersion) }
