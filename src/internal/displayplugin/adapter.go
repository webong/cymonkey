// Package displayplugin adapts an installed ext package that serves the
// display.engine contract to the host's engine interface. The host supplies
// the target and decides what the plugin may do; the plugin does neither.
package displayplugin

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host"
	"github.com/webong/ext/pkg/plugin-go/contract/host/process"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	"jangolova/displaykit"
)

const (
	ContractName    = displaykit.ContractName
	ContractVersion = displaykit.ContractVersion

	pollInterval = 250 * time.Millisecond
	callTimeout  = 2 * time.Second
)

// Operations lists every method a display.engine package serves.
var Operations = displaykit.Operations

// Policy is the host's endpoint and credential policy. The engine package asks
// for it through the display.host reverse contract instead of carrying its own
// copy, so there is one definition of what an endpoint may be and of which
// strings are secret.
type Policy interface {
	ValidateEndpoint(orchestrator.TargetEndpoint) error
	WorkerEnvironment(endpoint orchestrator.TargetEndpoint, environment []string) ([]string, error)
	Redact(message string, target orchestrator.EngineTarget) string
}

// Adapter runs one installed package as a display engine. Each Connect starts
// its own process, so sessions never share plugin state. Without a Policy the
// package is offered no host services.
type Adapter struct {
	Installed store.Installed
	Policy    Policy
	// Environment adds "NAME=value" entries to the package's scrubbed launch
	// environment, for example the path of the browser worker script.
	Environment []string
}

// launch returns how to start the package, or nil to use its default.
func (a Adapter) launch() (*process.Command, error) {
	if len(a.Environment) == 0 {
		return nil, nil
	}
	executable, err := a.Installed.Executable()
	if err != nil {
		return nil, err
	}
	return &process.Command{Path: executable, Env: a.Environment}, nil
}

// maxRedactBytes bounds a message the engine asks the host to redact.
const maxRedactBytes = 64 << 10

// reverseOptions serves the display.host contract for one connected target. It
// admits only the declared operations, and answers from the host's own copy of
// the target, so a request can name an endpoint but never supply its material.
func reverseOptions(policy Policy, target orchestrator.EngineTarget) *host.ReverseOptions {
	if policy == nil {
		return nil
	}
	find := func(requested displaykit.Endpoint) (orchestrator.TargetEndpoint, error) {
		for _, endpoint := range target.Endpoints {
			if endpoint.Name == requested.Name && endpoint.Protocol == requested.Protocol && endpoint.URL == requested.URL {
				return endpoint, nil
			}
		}
		return orchestrator.TargetEndpoint{}, errors.New("endpoint is not part of the connected target")
	}
	refuse := func(err error) error {
		return &plugin.RemoteError{Code: "policy", Message: policy.Redact(err.Error(), target)}
	}
	return &host.ReverseOptions{
		Authorize: func(_ context.Context, request plugin.Request) error {
			if request.Contract.Name != displaykit.HostContractName || request.Contract.Version != displaykit.HostContractVersion {
				return plugin.ErrDenied
			}
			for _, operation := range displaykit.HostOperations {
				if request.Operation == operation {
					return nil
				}
			}
			return plugin.ErrDenied
		},
		Handle: func(_ context.Context, request plugin.Request) (json.RawMessage, error) {
			switch request.Operation {
			case "validate_endpoint":
				var body displaykit.EndpointRequest
				if err := json.Unmarshal(request.Payload, &body); err != nil {
					return nil, errors.New("decode validate_endpoint")
				}
				endpoint, err := find(body.Endpoint)
				if err != nil {
					return nil, refuse(err)
				}
				if err := policy.ValidateEndpoint(endpoint); err != nil {
					return nil, refuse(err)
				}
				return json.RawMessage(`{}`), nil
			case "worker_environment":
				var body displaykit.EnvironmentRequest
				if err := json.Unmarshal(request.Payload, &body); err != nil {
					return nil, errors.New("decode worker_environment")
				}
				endpoint, err := find(body.Endpoint)
				if err != nil {
					return nil, refuse(err)
				}
				environment, err := policy.WorkerEnvironment(endpoint, body.Environment)
				if err != nil {
					return nil, refuse(err)
				}
				return json.Marshal(displaykit.EnvironmentResult{Environment: environment})
			case "redact":
				var body displaykit.RedactRequest
				if err := json.Unmarshal(request.Payload, &body); err != nil || len(body.Message) > maxRedactBytes {
					return nil, errors.New("decode redact")
				}
				return json.Marshal(displaykit.RedactResult{Message: policy.Redact(body.Message, target)})
			}
			return nil, plugin.ErrDenied
		},
	}
}

func method(name string) string { return ContractName + "." + name }

// InspectEngine reports availability from a short-lived process. A plugin
// that cannot start or answer is reported unavailable, never as an error.
func (a Adapter) InspectEngine(ctx context.Context) orchestrator.EngineInspection {
	proc, err := host.Open(ctx, a.Installed, host.Options{Authorize: probePolicy()})
	if err != nil {
		return orchestrator.EngineInspection{Message: err.Error()}
	}
	defer proc.Close()
	var result displaykit.Inspection
	if err := proc.Call(ctx, method("inspect"), nil, &result); err != nil {
		return orchestrator.EngineInspection{Message: err.Error()}
	}
	return orchestrator.EngineInspection{Available: result.Available, Capabilities: result.Capabilities, Message: result.Message}
}

func wireConnection(snapshot orchestrator.EndpointConnectionSnapshot) displaykit.Connection {
	connection := displaykit.Connection{
		Headers: snapshot.Headers, ExpiresAt: snapshot.ExpiresAt, Revision: snapshot.Revision,
		CredentialRevision: snapshot.CredentialRevision, TLSRevision: snapshot.TLSRevision,
	}
	if snapshot.TLS != nil {
		connection.TLS = &displaykit.TLS{
			CAFile: snapshot.TLS.CAFile, ClientCertificateFile: snapshot.TLS.ClientCertificateFile,
			ClientKeyFile: snapshot.TLS.ClientKeyFile, ServerName: snapshot.TLS.ServerName,
		}
	}
	return connection
}

// wireEndpointOf carries an endpoint with its prepared material. The headers
// are sensitive: they are what the engine presents to the target. The list of
// secrets to redact stays with the host; the engine asks for redaction.
func wireEndpointOf(endpoint orchestrator.TargetEndpoint) displaykit.Endpoint {
	wire := displaykit.Endpoint{
		Name: endpoint.Name, Protocol: endpoint.Protocol, URL: endpoint.URL,
		CredentialRef: endpoint.CredentialRef, TLSRef: endpoint.TLSRef,
		Audience: endpoint.Audience, Metadata: endpoint.Metadata,
	}
	if endpoint.Connection != nil {
		connection := wireConnection(endpoint.Connection.Snapshot())
		wire.Connection = &connection
	}
	return wire
}

// Connect starts the package and opens an engine session on target.
func (a Adapter) Connect(ctx context.Context, spec manifest.EngineSpec, target orchestrator.EngineTarget) (orchestrator.EngineInstance, error) {
	launch, err := a.launch()
	if err != nil {
		return nil, err
	}
	proc, err := host.Open(ctx, a.Installed, host.Options{Authorize: sessionPolicy(), Reverse: reverseOptions(a.Policy, target), Launch: launch})
	if err != nil {
		return nil, err
	}
	wire := displaykit.Target{APIVersion: target.APIVersion, TargetID: target.TargetID, Kind: target.Kind, Handles: target.Handles, Metadata: target.Metadata}
	for _, endpoint := range target.Endpoints {
		wire.Endpoints = append(wire.Endpoints, wireEndpointOf(endpoint))
	}
	var connected displaykit.ConnectResult
	request := displaykit.ConnectRequest{Spec: displaykit.Spec{
		Adapter: spec.Adapter, RequiredCapabilities: spec.RequiredCapabilities, Source: spec.Source, Options: spec.Options,
	}, Target: wire}
	if err := proc.Call(ctx, method("connect"), request, &connected); err != nil {
		_ = proc.Close()
		return nil, err
	}
	s := &session{
		proc: proc, capabilities: connected.Capabilities,
		launch: orchestrator.CallerLaunch{Environment: connected.CallerLaunch.Environment},
		events: make(chan orchestrator.EngineEvent, 8), done: make(chan struct{}),
	}
	for index, endpoint := range target.Endpoints {
		if endpoint.Connection != nil {
			s.wg.Add(1)
			go s.watchMaterial(index, endpoint.Connection)
		}
	}
	s.wg.Add(1)
	go s.pollEvents()
	return s, nil
}

type session struct {
	proc         *host.Process
	capabilities []string
	launch       orchestrator.CallerLaunch
	events       chan orchestrator.EngineEvent
	done         chan struct{}
	once         sync.Once
	wg           sync.WaitGroup
}

func (s *session) Call(ctx context.Context, name string, params json.RawMessage) (json.RawMessage, error) {
	var result json.RawMessage
	err := s.proc.Call(ctx, method("call"), displaykit.CallRequest{Method: name, Params: params}, &result)
	return result, err
}

func (s *session) Authorize(ctx context.Context, request orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	var decision displaykit.AuthorizeDecision
	err := s.proc.Call(ctx, method("authorize"), displaykit.AuthorizeRequest{
		TargetID: request.TargetID, Action: request.Action, Input: request.Input, Capabilities: request.Capabilities,
	}, &decision)
	return orchestrator.AuthorizeDecision{Authorized: decision.Authorized, Reason: decision.Reason}, err
}

func (s *session) EngineCapabilities() []string { return append([]string(nil), s.capabilities...) }

func (s *session) EngineCallerLaunch() orchestrator.CallerLaunch { return s.launch }

func (s *session) EngineEvents() <-chan orchestrator.EngineEvent { return s.events }

func (s *session) EngineHealth(ctx context.Context) orchestrator.EngineHealth {
	var health displaykit.Health
	if err := s.proc.Call(ctx, method("health"), nil, &health); err != nil {
		return orchestrator.EngineHealth{Status: orchestrator.EngineHealthUnhealthy, Message: err.Error(), ObservedAt: time.Now().UTC()}
	}
	return orchestrator.EngineHealth{Status: health.Status, Message: health.Message, ObservedAt: health.ObservedAt}
}

func (s *session) Disconnect(ctx context.Context) error {
	s.once.Do(func() { close(s.done) })
	s.wg.Wait()
	err := s.proc.Call(ctx, method("disconnect"), nil, nil)
	return errors.Join(err, s.proc.Close())
}

func (s *session) pollEvents() {
	defer s.wg.Done()
	defer close(s.events)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var cursor string
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		var result displaykit.EventsResult
		err := s.proc.Call(ctx, method("events"), displaykit.EventsRequest{Cursor: cursor}, &result)
		cancel()
		if err != nil {
			select {
			case s.events <- orchestrator.EngineEvent{Type: "cymonkey.failed", Status: orchestrator.EngineHealthUnhealthy, Message: err.Error(), OccurredAt: time.Now().UTC()}:
			default:
			}
			return
		}
		cursor = result.Cursor
		for _, event := range result.Events {
			select {
			case <-s.done:
				return
			case s.events <- orchestrator.EngineEvent{Type: event.Type, Status: event.Status, Message: event.Message, OccurredAt: event.OccurredAt}:
			}
		}
	}
}

// watchMaterial forwards each new revision of an endpoint's connection
// material and acknowledges it once the plugin has accepted it.
func (s *session) watchMaterial(index int, material *orchestrator.EndpointConnection) {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case _, open := <-material.Updates():
			if !open {
				return
			}
			snapshot := material.Snapshot()
			update := displaykit.MaterialUpdate{Endpoint: index, Connection: wireConnection(snapshot)}
			ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
			err := s.proc.Call(ctx, method("material"), update, nil)
			cancel()
			if err != nil {
				return
			}
			material.Acknowledge(snapshot.Revision)
		}
	}
}
