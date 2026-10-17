package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"

	"jangolova/displaykit/bridge"
	"jangolova/displaykit/nodeworker"
	"jangolova/displaykit/sdk"
)

// errNeedsHost reports a service that depends on the host's policy or
// credential handling, which this engine cannot reach until ext provides
// guest-to-host calls.
var errNeedsHost = errors.New("this display engine service requires the host, which is not available to a display plugin yet")

func guestServices() sdk.Host {
	return sdk.Host{
		ValidateEndpoint:  func(sdk.TargetEndpoint) error { return errNeedsHost },
		ResolveWorker:     resolveWorker,
		WorkerEnvironment: func(sdk.TargetEndpoint, []string) ([]string, error) { return nil, errNeedsHost },
		StartWorker: func(node, worker string, args, environment []string) (sdk.Worker, error) {
			return nodeworker.Start(node, worker, args, environment)
		},
		DialWebSocket: func(context.Context, sdk.TargetEndpoint) (*websocket.Conn, error) { return nil, errNeedsHost },
		ListenWebSocket: func(address string) (sdk.Listener, error) {
			host, err := bridge.NewWebSocketHost(address)
			if err != nil {
				return nil, err
			}
			return listener{host}, nil
		},
		ConnectSafari: func(context.Context, sdk.EngineSpec, sdk.EngineTarget) (sdk.EngineInstance, error) {
			return nil, errNeedsHost
		},
		// A redacting service must never pass the message through, because the
		// engine cannot see which strings are secret.
		Redact: func(string, sdk.EngineTarget) string { return "display engine operation failed" },
	}
}

type listener struct{ source *bridge.WebSocketHost }

func (l listener) Endpoint() string                { return l.source.Endpoint() }
func (l listener) Token() string                   { return l.source.Token() }
func (l listener) Close(ctx context.Context) error { return l.source.Close(ctx) }
func (l listener) WaitConnection(ctx context.Context) (sdk.Transport, error) {
	return l.source.WaitConnection(ctx)
}

// resolveWorker finds the browser worker script. The path comes from
// CYMONKEY_WORKER, which the host passes when it launches the package.
func resolveWorker(kind string) (string, error) {
	if kind != "browser" {
		return "", errors.New("unsupported worker kind")
	}
	candidates := []string{strings.TrimSpace(os.Getenv("CYMONKEY_WORKER"))}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "cymonkey-worker.mjs"))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Abs(candidate)
		}
	}
	return "", errors.New("Cymonkey browser worker not found; set CYMONKEY_WORKER")
}
