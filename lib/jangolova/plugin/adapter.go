// Package jangolovaplugin adapts an installed executable to the public
// Jangolova engine interface. The host still supplies targets and policy.
package jangolovaplugin

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"jangolova/sdk"
	"providerplugin"
)

type Adapter struct{ Installed providerplugin.Installed }

const Kind = "jangolova.engine"

type NamedAdapter struct {
	Name    string
	Adapter Adapter
}

// Adapters selects Jangolova engines from a shared installation inventory.
func Adapters(installed []providerplugin.Installed) []NamedAdapter {
	var adapters []NamedAdapter
	for _, item := range installed {
		if item.Manifest.Kind == Kind {
			adapters = append(adapters, NamedAdapter{Name: item.Manifest.Name, Adapter: Adapter{Installed: item}})
		}
	}
	return adapters
}

func (a Adapter) InspectEngine(ctx context.Context) sdk.EngineInspection {
	proc, err := providerplugin.Open(ctx, a.Installed)
	if err != nil {
		return sdk.EngineInspection{Available: false, Message: err.Error()}
	}
	defer proc.Close()
	var inspection sdk.EngineInspection
	if err := proc.Call(ctx, "jangolova.inspect", nil, &inspection); err != nil {
		return sdk.EngineInspection{Available: false, Message: err.Error()}
	}
	return inspection
}

type wireEndpoint struct {
	Name          string                         `json:"name"`
	Protocol      string                         `json:"protocol"`
	URL           string                         `json:"url"`
	CredentialRef string                         `json:"credentialRef,omitempty"`
	TLSRef        string                         `json:"tlsRef,omitempty"`
	Audience      string                         `json:"audience,omitempty"`
	Metadata      map[string]string              `json:"metadata,omitempty"`
	Connection    sdk.EndpointConnectionSnapshot `json:"connection"`
}
type wireTarget struct {
	APIVersion string            `json:"apiVersion"`
	TargetID   string            `json:"targetId"`
	Kind       string            `json:"kind"`
	Endpoints  []wireEndpoint    `json:"endpoints"`
	Handles    map[string]string `json:"handles,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

func (a Adapter) Connect(ctx context.Context, spec sdk.EngineSpec, target sdk.EngineTarget) (sdk.EngineInstance, error) {
	proc, err := providerplugin.Open(ctx, a.Installed)
	if err != nil {
		return nil, err
	}
	wire := wireTarget{APIVersion: target.APIVersion, TargetID: target.TargetID, Kind: target.Kind, Handles: target.Handles, Metadata: target.Metadata}
	for _, endpoint := range target.Endpoints {
		wire.Endpoints = append(wire.Endpoints, wireEndpoint{Name: endpoint.Name, Protocol: endpoint.Protocol, URL: endpoint.URL, CredentialRef: endpoint.CredentialRef, TLSRef: endpoint.TLSRef, Audience: endpoint.Audience, Metadata: endpoint.Metadata, Connection: endpoint.Snapshot()})
	}
	var connected struct {
		Capabilities []string         `json:"capabilities"`
		CallerLaunch sdk.CallerLaunch `json:"callerLaunch"`
	}
	if err := proc.Call(ctx, "jangolova.connect", map[string]any{"spec": spec, "target": wire}, &connected); err != nil {
		_ = proc.Close()
		return nil, err
	}
	s := &session{proc: proc, target: target, capabilities: connected.Capabilities, launch: connected.CallerLaunch, events: make(chan sdk.EngineEvent, 8), done: make(chan struct{})}
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
	proc         *providerplugin.Process
	target       sdk.EngineTarget
	capabilities []string
	launch       sdk.CallerLaunch
	events       chan sdk.EngineEvent
	done         chan struct{}
	once         sync.Once
	wg           sync.WaitGroup
}

func (s *session) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	var result json.RawMessage
	err := s.proc.Call(ctx, "jangolova.call", map[string]any{"method": method, "params": params}, &result)
	return result, err
}
func (s *session) Authorize(ctx context.Context, request sdk.AuthorizeRequest) (sdk.AuthorizeDecision, error) {
	var decision sdk.AuthorizeDecision
	err := s.proc.Call(ctx, "jangolova.authorize", request, &decision)
	return decision, err
}
func (s *session) EngineCapabilities() []string         { return append([]string(nil), s.capabilities...) }
func (s *session) EngineCallerLaunch() sdk.CallerLaunch { return s.launch }
func (s *session) EngineEvents() <-chan sdk.EngineEvent { return s.events }
func (s *session) EngineHealth(ctx context.Context) sdk.EngineHealth {
	var health sdk.EngineHealth
	if err := s.proc.Call(ctx, "jangolova.health", nil, &health); err != nil {
		return sdk.EngineHealth{Status: sdk.EngineHealthUnhealthy, Message: err.Error(), ObservedAt: time.Now().UTC()}
	}
	return health
}
func (s *session) Disconnect(ctx context.Context) error {
	s.once.Do(func() { close(s.done) })
	s.wg.Wait()
	err := s.proc.Call(ctx, "jangolova.disconnect", nil, nil)
	closeErr := s.proc.Close()
	return errors.Join(err, closeErr)
}
func (s *session) pollEvents() {
	defer s.wg.Done()
	defer close(s.events)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var cursor string
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		var result struct {
			Cursor string            `json:"cursor"`
			Events []sdk.EngineEvent `json:"events"`
		}
		err := s.proc.Call(ctx, "jangolova.events", map[string]string{"cursor": cursor}, &result)
		cancel()
		if err != nil {
			select {
			case s.events <- sdk.EngineEvent{Type: "jangolova.failed", Status: sdk.EngineHealthUnhealthy, Message: err.Error(), OccurredAt: time.Now().UTC()}:
			default:
			}
			return
		}
		cursor = result.Cursor
		for _, event := range result.Events {
			select {
			case <-s.done:
				return
			case s.events <- event:
			}
		}
	}
}
func (s *session) watchMaterial(index int, material sdk.ConnectionMaterial) {
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
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := s.proc.Call(ctx, "jangolova.material", map[string]any{"endpoint": index, "connection": snapshot}, nil)
			cancel()
			if err != nil {
				return
			}
			material.Acknowledge(snapshot.Revision)
		}
	}
}
