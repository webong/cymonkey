package displaykit

import plugin "github.com/webong/ext/pkg/plugin-go/contract"

// The display.host contract is what a display engine package may ask of its
// host while a session is connected. The host owns endpoint policy, the worker
// environment for authenticated endpoints, and the list of secrets to keep out
// of anything the engine reports, so the engine asks instead of re-implementing
// them. A package declares these operations in its descriptor's reverse list.
const (
	HostContractName    = "display.host"
	HostContractVersion = "v1"
)

// HostOperations lists every operation the host serves to a display engine.
var HostOperations = []string{"validate_endpoint", "worker_environment", "redact"}

// HostContract is the reverse contract a display engine package declares.
func HostContract() plugin.Contract {
	operations := make([]plugin.Operation, 0, len(HostOperations))
	for _, name := range HostOperations {
		operations = append(operations, plugin.Operation{Name: name})
	}
	return plugin.Contract{
		ContractRef: plugin.ContractRef{Name: HostContractName, Version: HostContractVersion},
		Operations:  operations,
	}
}

// HostRef names the contract for a reverse call.
func HostRef() plugin.ContractRef {
	return plugin.ContractRef{Name: HostContractName, Version: HostContractVersion}
}

// EndpointRequest names one endpoint of the connected target. The host finds
// its own copy by name, protocol and URL; the engine's description of the
// endpoint is never trusted to carry credentials.
type EndpointRequest struct {
	Endpoint Endpoint `json:"endpoint"`
}

// EnvironmentRequest asks the host to prepare the environment of a worker
// process that will connect to an endpoint.
type EnvironmentRequest struct {
	Endpoint    Endpoint `json:"endpoint"`
	Environment []string `json:"environment,omitempty"`
}

type EnvironmentResult struct {
	Environment []string `json:"environment,omitempty"`
}

// RedactRequest asks the host to remove the target's secrets from a message.
type RedactRequest struct {
	Message string `json:"message"`
}

type RedactResult struct {
	Message string `json:"message"`
}
