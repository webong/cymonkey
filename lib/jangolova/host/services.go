// Package host defines the Jangolova-owned host boundary.
//
// The package contains no Cymonkey implementation details. Hosts inject the
// concrete endpoint, worker, transport, and redaction services they own.
package host

import (
	"context"

	"github.com/gorilla/websocket"

	"jangolova/sdk"
)

// Dependencies are the host services a Jangolova adapter may use. Each service
// is supplied explicitly; the package does not discover or launch a target.
type Dependencies struct {
	ValidateEndpoint  func(sdk.TargetEndpoint) error
	WorkerEnvironment func(sdk.TargetEndpoint, []string) ([]string, error)
	StartWorker       func(string, string, []string, []string) (sdk.Worker, error)
	DialWebSocket     func(context.Context, sdk.TargetEndpoint) (*websocket.Conn, error)
	ListenWebSocket   func(string) (sdk.Listener, error)
	ConnectSafari     func(context.Context, sdk.EngineSpec, sdk.EngineTarget) (sdk.EngineInstance, error)
	Redact            func(string, sdk.EngineTarget) string
}

// Services converts host-owned dependencies into the public SDK host shape.
func Services(dependencies Dependencies) sdk.Host {
	return sdk.Host{
		ValidateEndpoint:  dependencies.ValidateEndpoint,
		WorkerEnvironment: dependencies.WorkerEnvironment,
		StartWorker:       dependencies.StartWorker,
		DialWebSocket:     dependencies.DialWebSocket,
		ListenWebSocket:   dependencies.ListenWebSocket,
		ConnectSafari:     dependencies.ConnectSafari,
		Redact:            dependencies.Redact,
	}
}
