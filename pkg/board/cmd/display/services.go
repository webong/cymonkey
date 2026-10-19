package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"

	"jangolova/displaykit"
	"jangolova/displaykit/bridge"
	"jangolova/displaykit/nodeworker"
	"jangolova/displaykit/sdk"
)

// hostCallTimeout bounds one call to the host.
const hostCallTimeout = 15 * time.Second

// errNoHost reports a service that needs the host while no session is
// connected.
var errNoHost = errors.New("this display engine service requires a connected session")

// hostServices asks the host for the services that depend on its endpoint
// policy and credential handling. It uses the session-scoped reverse caller, so
// the engine can ask from its background work as well as from a call.
type hostServices struct {
	caller plugin.ReverseCaller
}

func (h hostServices) call(operation string, request, result any) error {
	if h.caller == nil {
		return errNoHost
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostCallTimeout)
	defer cancel()
	raw, err := h.caller.Call(ctx, displaykit.HostRef(), operation, payload)
	if err != nil {
		return err
	}
	if result == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, result)
}

// described is an endpoint as the host knows it, without connection material.
func described(endpoint sdk.TargetEndpoint) displaykit.Endpoint {
	return displaykit.Endpoint{
		Name: endpoint.Name, Protocol: endpoint.Protocol, URL: endpoint.URL,
		CredentialRef: endpoint.CredentialRef, TLSRef: endpoint.TLSRef,
		Audience: endpoint.Audience, Metadata: endpoint.Metadata,
	}
}

func (h hostServices) validate(endpoint sdk.TargetEndpoint) error {
	return h.call("validate_endpoint", displaykit.EndpointRequest{Endpoint: described(endpoint)}, nil)
}

func (h hostServices) workerEnvironment(endpoint sdk.TargetEndpoint, environment []string) ([]string, error) {
	var result displaykit.EnvironmentResult
	err := h.call("worker_environment", displaykit.EnvironmentRequest{Endpoint: described(endpoint), Environment: environment}, &result)
	return result.Environment, err
}

// redact removes the target's secrets from a message. If the host cannot be
// asked, the message is replaced rather than passed through: the engine cannot
// see which strings are secret.
func (h hostServices) redact(message string, _ sdk.EngineTarget) string {
	var result displaykit.RedactResult
	if err := h.call("redact", displaykit.RedactRequest{Message: message}, &result); err != nil {
		return "display engine operation failed"
	}
	return result.Message
}

// dial opens a WebSocket to an endpoint the host has approved, presenting the
// headers and certificate trust from the endpoint's prepared material.
func (h hostServices) dial(ctx context.Context, endpoint sdk.TargetEndpoint) (*websocket.Conn, error) {
	if err := h.validate(endpoint); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(endpoint.URL)
	if err != nil || parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return nil, errors.New("WebSocket endpoint must use ws or wss")
	}
	dialer := *websocket.DefaultDialer
	headers := http.Header{}
	if current, ok := endpoint.Connection.(*material); ok && current != nil {
		snapshot := current.connection()
		for name, value := range snapshot.Headers {
			headers.Set(name, value)
		}
		if snapshot.TLS != nil {
			if parsed.Scheme != "wss" {
				return nil, errors.New("TLS connection material requires a wss endpoint")
			}
			if dialer.TLSClientConfig, err = tlsConfig(snapshot.TLS); err != nil {
				return nil, err
			}
		}
	}
	connection, response, err := dialer.DialContext(ctx, endpoint.URL, headers)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, errors.New("connect to caller-owned display WebSocket endpoint")
	}
	return connection, nil
}

func tlsConfig(material *displaykit.TLS) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: material.ServerName}
	if material.CAFile != "" {
		data, err := os.ReadFile(material.CAFile)
		if err != nil {
			return nil, errors.New("read target CA file")
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return nil, errors.New("target CA file has no certificates")
		}
		config.RootCAs = pool
	}
	if (material.ClientCertificateFile == "") != (material.ClientKeyFile == "") {
		return nil, errors.New("TLS client certificate and key must be given together")
	}
	if material.ClientCertificateFile != "" {
		certificate, err := tls.LoadX509KeyPair(material.ClientCertificateFile, material.ClientKeyFile)
		if err != nil {
			return nil, errors.New("load target client certificate")
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}

// guestServices builds the engine's host services. caller is the session-scoped
// reverse caller, or nil when no session is connected (as when inspecting).
func guestServices(caller plugin.ReverseCaller) sdk.Host {
	host := hostServices{caller: caller}
	return sdk.Host{
		ValidateEndpoint:  host.validate,
		ResolveWorker:     resolveWorker,
		WorkerEnvironment: host.workerEnvironment,
		StartWorker: func(node, worker string, args, environment []string) (sdk.Worker, error) {
			return nodeworker.Start(node, worker, args, environment)
		},
		DialWebSocket: host.dial,
		ListenWebSocket: func(address string) (sdk.Listener, error) {
			listening, err := bridge.NewWebSocketHost(address)
			if err != nil {
				return nil, err
			}
			return listener{listening}, nil
		},
		// The Safari bridge is a host engine of its own (safari-mcp); it is not
		// offered through this package.
		ConnectSafari: func(context.Context, sdk.EngineSpec, sdk.EngineTarget) (sdk.EngineInstance, error) {
			return nil, errors.New("the Safari bridge is served by the safari-mcp engine, not this package")
		},
		Redact: host.redact,
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
