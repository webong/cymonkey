package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"board/display"
	"jangolova/displaykit"
	"jangolova/displaykit/sdk"
	"jangolova/displaykit/serve"
)

type engine struct{ adapter display.Adapter }

func newEngine() engine {
	return engine{adapter: display.Adapter{Host: guestServices()}}
}

func (e engine) Inspect(ctx context.Context) displaykit.Inspection {
	result := e.adapter.InspectEngine(ctx)
	return displaykit.Inspection{Available: result.Available, Capabilities: result.Capabilities, Message: result.Message}
}

func (e engine) Connect(ctx context.Context, spec displaykit.Spec, target displaykit.Target) (serve.Session, error) {
	materials := make([]*material, len(target.Endpoints))
	libraryTarget := sdk.EngineTarget{
		APIVersion: target.APIVersion, TargetID: target.TargetID, Kind: target.Kind,
		Handles: target.Handles, Metadata: target.Metadata,
	}
	for index, endpoint := range target.Endpoints {
		converted := sdk.TargetEndpoint{
			Name: endpoint.Name, Protocol: endpoint.Protocol, URL: endpoint.URL,
			CredentialRef: endpoint.CredentialRef, TLSRef: endpoint.TLSRef,
			Audience: endpoint.Audience, Metadata: endpoint.Metadata,
		}
		if endpoint.Connection != nil {
			materials[index] = newMaterial(*endpoint.Connection)
			converted.Connection = materials[index]
		}
		libraryTarget.Endpoints = append(libraryTarget.Endpoints, converted)
	}
	instance, err := e.adapter.Connect(ctx, sdk.EngineSpec{
		Adapter: spec.Adapter, RequiredCapabilities: spec.RequiredCapabilities, Source: spec.Source, Options: spec.Options,
	}, libraryTarget)
	if err != nil {
		return nil, err
	}
	return newSession(instance, materials), nil
}

// session adapts a library engine instance to the wire session. The library
// names its events and launch variables after itself; the host-facing names
// are Cymonkey's.
type session struct {
	instance  sdk.EngineInstance
	materials []*material
	events    chan displaykit.Event
	done      chan struct{}
	once      sync.Once
}

func newSession(instance sdk.EngineInstance, materials []*material) *session {
	s := &session{instance: instance, materials: materials, events: make(chan displaykit.Event, 8), done: make(chan struct{})}
	go s.forwardEvents()
	return s
}

func (s *session) forwardEvents() {
	defer close(s.events)
	source, ok := s.instance.(sdk.EngineEventSource)
	if !ok {
		<-s.done
		return
	}
	for {
		select {
		case <-s.done:
			return
		case event, open := <-source.EngineEvents():
			if !open {
				return
			}
			translated := displaykit.Event{Type: hostEventName(event.Type), Status: event.Status, Message: event.Message, OccurredAt: event.OccurredAt}
			select {
			case s.events <- translated:
			case <-s.done:
				return
			}
		}
	}
}

func hostEventName(name string) string {
	if rest, ok := strings.CutPrefix(name, "jangolova."); ok {
		return "cymonkey." + rest
	}
	return name
}

func (s *session) Capabilities() []string {
	if provider, ok := s.instance.(sdk.EngineCapabilityProvider); ok {
		return provider.EngineCapabilities()
	}
	return nil
}

var launchVariables = map[string]string{
	"JANGOLOVA_CONTROL_URL":      "CYMONKEY_CONTROL_URL",
	"JANGOLOVA_CONTROL_TOKEN":    "CYMONKEY_CONTROL_TOKEN",
	"JANGOLOVA_CONTROL_PROTOCOL": "CYMONKEY_PROTOCOL",
}

func (s *session) CallerLaunch() map[string]string {
	provider, ok := s.instance.(sdk.EngineCallerLaunchProvider)
	if !ok {
		return nil
	}
	environment := map[string]string{}
	for name, value := range provider.EngineCallerLaunch().Environment {
		if renamed, ok := launchVariables[name]; ok {
			name = renamed
		}
		environment[name] = value
	}
	return environment
}

func (s *session) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	caller, ok := s.instance.(sdk.Caller)
	if !ok {
		return nil, errors.New("module does not implement semantic calls")
	}
	return caller.Call(ctx, method, params)
}

func (s *session) Authorize(ctx context.Context, request displaykit.AuthorizeRequest) (displaykit.AuthorizeDecision, error) {
	decision, err := s.instance.Authorize(ctx, sdk.AuthorizeRequest{
		TargetID: request.TargetID, Action: request.Action, Input: request.Input, Capabilities: request.Capabilities,
	})
	return displaykit.AuthorizeDecision{Authorized: decision.Authorized, Reason: decision.Reason}, err
}

func (s *session) Health(ctx context.Context) displaykit.Health {
	if provider, ok := s.instance.(sdk.EngineHealthProvider); ok {
		health := provider.EngineHealth(ctx)
		return displaykit.Health{Status: health.Status, Message: health.Message, ObservedAt: health.ObservedAt}
	}
	return displaykit.Health{Status: sdk.EngineHealthUnknown, ObservedAt: time.Now().UTC()}
}

func (s *session) Events() <-chan displaykit.Event { return s.events }

func (s *session) UpdateMaterial(endpoint int, connection displaykit.Connection) error {
	if endpoint < 0 || endpoint >= len(s.materials) || s.materials[endpoint] == nil {
		return errors.New("endpoint has no connection material")
	}
	s.materials[endpoint].update(connection)
	return nil
}

func (s *session) Disconnect(ctx context.Context) error {
	err := s.instance.Disconnect(ctx)
	s.once.Do(func() { close(s.done) })
	return err
}
