// Cymonkey composes private host services with the standalone display library
// at the executable edge. Internal packages receive only neutral interfaces.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	displayhost "board/display/host"
	displaysdk "board/display/sdk"
	"cymonkey/src/adapters/safarimcp"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	"cymonkey/src/internal/targetconn"
	"github.com/gorilla/websocket"
	"jangolova/displaykit/bridge"
	"jangolova/displaykit/nodeworker"
)

type materialView struct {
	source *orchestrator.EndpointConnection
}

func (v materialView) Snapshot() displaysdk.EndpointConnectionSnapshot {
	s := v.source.Snapshot()
	return displaysdk.EndpointConnectionSnapshot{Headers: s.Headers, Revision: s.Revision, CredentialRevision: s.CredentialRevision, TLSRevision: s.TLSRevision}
}
func (v materialView) Updates() <-chan uint64 { return v.source.Updates() }
func (v materialView) Acknowledge(r uint64)   { v.source.Acknowledge(r) }
func displayLibraryTarget(t orchestrator.EngineTarget) displaysdk.EngineTarget {
	result := displaysdk.EngineTarget{APIVersion: t.APIVersion, TargetID: t.TargetID, Kind: t.Kind, Handles: maps.Clone(t.Handles), Metadata: maps.Clone(t.Metadata)}
	for _, e := range t.Endpoints {
		next := displaysdk.TargetEndpoint{Name: e.Name, Protocol: e.Protocol, URL: e.URL, CredentialRef: e.CredentialRef, TLSRef: e.TLSRef, Audience: e.Audience, Metadata: maps.Clone(e.Metadata)}
		if e.Connection != nil {
			next.Connection = materialView{e.Connection}
		}
		result.Endpoints = append(result.Endpoints, next)
	}
	return result
}
func endpoint(e displaysdk.TargetEndpoint) (orchestrator.TargetEndpoint, error) {
	result := orchestrator.TargetEndpoint{Name: e.Name, Protocol: e.Protocol, URL: e.URL, CredentialRef: e.CredentialRef, TLSRef: e.TLSRef, Audience: e.Audience, Metadata: e.Metadata}
	if e.Connection != nil {
		view, ok := e.Connection.(materialView)
		if !ok {
			return result, errors.New("endpoint material was not supplied by this host")
		}
		result.Connection = view.source
	}
	return result, nil
}
func coordinatorTarget(t displaysdk.EngineTarget) (orchestrator.EngineTarget, error) {
	result := orchestrator.EngineTarget{APIVersion: t.APIVersion, TargetID: t.TargetID, Kind: t.Kind, Handles: t.Handles, Metadata: t.Metadata}
	for _, e := range t.Endpoints {
		next, err := endpoint(e)
		if err != nil {
			return result, err
		}
		result.Endpoints = append(result.Endpoints, next)
	}
	return result, nil
}

type listener struct{ source *bridge.WebSocketHost }

func (l listener) Endpoint() string                { return l.source.Endpoint() }
func (l listener) Token() string                   { return l.source.Token() }
func (l listener) Close(ctx context.Context) error { return l.source.Close(ctx) }
func (l listener) WaitConnection(ctx context.Context) (displaysdk.Transport, error) {
	return l.source.WaitConnection(ctx)
}

func displayLibraryServices() displaysdk.Host {
	return displayhost.Services(displayhost.Dependencies{
		ValidateEndpoint: func(e displaysdk.TargetEndpoint) error {
			p, err := endpoint(e)
			if err != nil {
				return err
			}
			return targetconn.Validate(p)
		},
		ResolveWorker: resolveBrowserWorker,
		WorkerEnvironment: func(e displaysdk.TargetEndpoint, env []string) ([]string, error) {
			p, err := endpoint(e)
			if err != nil {
				return nil, err
			}
			return targetconn.NodeEnvironment(p, env)
		},
		StartWorker: func(n, w string, args, env []string) (displaysdk.Worker, error) {
			return nodeworker.Start(n, w, args, env)
		},
		DialWebSocket: func(ctx context.Context, e displaysdk.TargetEndpoint) (*websocket.Conn, error) {
			p, err := endpoint(e)
			if err != nil {
				return nil, err
			}
			dialer, values, err := targetconn.WebSocketDialer(p)
			if err != nil {
				return nil, err
			}
			headers := http.Header{}
			for k, v := range values {
				headers.Set(k, v)
			}
			conn, response, err := dialer.DialContext(ctx, e.URL, headers)
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			if err != nil {
				return nil, errors.New("connect to caller-owned display WebSocket endpoint")
			}
			return conn, nil
		},
		ListenWebSocket: func(address string) (displaysdk.Listener, error) {
			h, err := bridge.NewWebSocketHost(address)
			if err != nil {
				return nil, err
			}
			return listener{h}, nil
		},
		ConnectSafari: func(ctx context.Context, s displaysdk.EngineSpec, t displaysdk.EngineTarget) (displaysdk.EngineInstance, error) {
			target, err := coordinatorTarget(t)
			if err != nil {
				return nil, err
			}
			i, err := (safarimcp.Adapter{}).Connect(ctx, manifest.EngineSpec{Adapter: s.Adapter, RequiredCapabilities: s.RequiredCapabilities, Source: s.Source, Options: s.Options}, target)
			if err != nil {
				return nil, err
			}
			return &safariSession{source: i}, nil
		},
		Redact: func(message string, t displaysdk.EngineTarget) string {
			p, err := coordinatorTarget(t)
			if err != nil {
				return "host operation failed"
			}
			return targetconn.RedactString(message, p)
		},
	})
}

// wrapDisplayLibrary adapts data across the public boundary explicitly; no private type is
// re-exported or used in a public module signature.
func wrapDisplayLibrary(adapter displaysdk.EngineAdapter) orchestrator.EngineAdapter {
	return adapterBinding{adapter}
}

type adapterBinding struct{ source displaysdk.EngineAdapter }

func (a adapterBinding) InspectEngine(ctx context.Context) orchestrator.EngineInspection {
	if p, ok := a.source.(displaysdk.EngineInspector); ok {
		v := p.InspectEngine(ctx)
		return orchestrator.EngineInspection{Available: v.Available, Capabilities: v.Capabilities, Message: v.Message}
	}
	return orchestrator.EngineInspection{Available: true}
}
func (a adapterBinding) Connect(ctx context.Context, s manifest.EngineSpec, t orchestrator.EngineTarget) (orchestrator.EngineInstance, error) {
	i, err := a.source.Connect(ctx, displaysdk.EngineSpec{Adapter: s.Adapter, RequiredCapabilities: s.RequiredCapabilities, Source: s.Source, Options: s.Options}, displayLibraryTarget(t))
	if err != nil {
		return nil, err
	}
	return &sessionBinding{source: i, done: make(chan struct{})}, nil
}

type sessionBinding struct {
	source     displaysdk.EngineInstance
	done       chan struct{}
	closeOnce  sync.Once
	eventsOnce sync.Once
	events     chan orchestrator.EngineEvent
}

func (i *sessionBinding) Disconnect(ctx context.Context) error {
	err := i.source.Disconnect(ctx)
	i.closeOnce.Do(func() { close(i.done) })
	return err
}
func (i *sessionBinding) Authorize(ctx context.Context, r orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	v, err := i.source.Authorize(ctx, displaysdk.AuthorizeRequest{TargetID: r.TargetID, Action: r.Action, Input: r.Input, Capabilities: r.Capabilities})
	return orchestrator.AuthorizeDecision{Authorized: v.Authorized, Reason: v.Reason}, err
}
func (i *sessionBinding) Call(ctx context.Context, m string, p json.RawMessage) (json.RawMessage, error) {
	if c, ok := i.source.(displaysdk.Caller); ok {
		return c.Call(ctx, m, p)
	}
	return nil, errors.New("module does not implement semantic calls")
}
func (i *sessionBinding) EngineCapabilities() []string {
	if p, ok := i.source.(displaysdk.EngineCapabilityProvider); ok {
		return p.EngineCapabilities()
	}
	return nil
}
func (i *sessionBinding) EngineHealth(ctx context.Context) orchestrator.EngineHealth {
	if p, ok := i.source.(displaysdk.EngineHealthProvider); ok {
		v := p.EngineHealth(ctx)
		return orchestrator.EngineHealth{Status: v.Status, Message: v.Message, ObservedAt: v.ObservedAt}
	}
	return orchestrator.EngineHealth{Status: orchestrator.EngineHealthUnknown, ObservedAt: time.Now().UTC()}
}
func (i *sessionBinding) EngineCallerLaunch() orchestrator.CallerLaunch {
	if p, ok := i.source.(displaysdk.EngineCallerLaunchProvider); ok {
		environment := maps.Clone(p.EngineCallerLaunch().Environment)
		if value := environment["JANGOLOVA_CONTROL_URL"]; value != "" {
			environment["CYMONKEY_CONTROL_URL"] = value
			delete(environment, "JANGOLOVA_CONTROL_URL")
		}
		if value := environment["JANGOLOVA_CONTROL_TOKEN"]; value != "" {
			environment["CYMONKEY_CONTROL_TOKEN"] = value
			delete(environment, "JANGOLOVA_CONTROL_TOKEN")
		}
		if value := environment["JANGOLOVA_CONTROL_PROTOCOL"]; value != "" {
			environment["CYMONKEY_PROTOCOL"] = value
			delete(environment, "JANGOLOVA_CONTROL_PROTOCOL")
		}
		return orchestrator.CallerLaunch{Environment: environment}
	}
	return orchestrator.CallerLaunch{}
}
func (i *sessionBinding) EngineEvents() <-chan orchestrator.EngineEvent {
	i.eventsOnce.Do(func() {
		i.events = make(chan orchestrator.EngineEvent, 8)
		source, ok := i.source.(displaysdk.EngineEventSource)
		if !ok {
			close(i.events)
			return
		}
		go func() {
			defer close(i.events)
			for {
				select {
				case <-i.done:
					return
				case e, open := <-source.EngineEvents():
					if !open {
						return
					}
					eventType := e.Type
					if strings.HasPrefix(eventType, "jangolova.") {
						eventType = "cymonkey." + strings.TrimPrefix(eventType, "jangolova.")
					}
					v := orchestrator.EngineEvent{Type: eventType, Status: e.Status, Message: e.Message, OccurredAt: e.OccurredAt}
					select {
					case i.events <- v:
					case <-i.done:
						return
					}
				}
			}
		}()
	})
	return i.events
}

type safariSession struct{ source orchestrator.EngineInstance }

func (i *safariSession) Disconnect(ctx context.Context) error { return i.source.Disconnect(ctx) }
func (i *safariSession) Authorize(ctx context.Context, r displaysdk.AuthorizeRequest) (displaysdk.AuthorizeDecision, error) {
	v, err := i.source.Authorize(ctx, orchestrator.AuthorizeRequest{TargetID: r.TargetID, Action: r.Action, Input: r.Input, Capabilities: r.Capabilities})
	return displaysdk.AuthorizeDecision{Authorized: v.Authorized, Reason: v.Reason}, err
}
func (i *safariSession) Call(ctx context.Context, m string, p json.RawMessage) (json.RawMessage, error) {
	if c, ok := i.source.(bridge.Caller); ok {
		return c.Call(ctx, m, p)
	}
	return nil, errors.New("Safari adapter does not implement semantic calls")
}
func (i *safariSession) EngineHealth(ctx context.Context) displaysdk.EngineHealth {
	if p, ok := i.source.(orchestrator.EngineHealthProvider); ok {
		v := p.EngineHealth(ctx)
		return displaysdk.EngineHealth{Status: v.Status, Message: v.Message, ObservedAt: v.ObservedAt}
	}
	return displaysdk.EngineHealth{Status: displaysdk.EngineHealthUnknown}
}
