package cymonkey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"jangolova/internal/bridge"
	"jangolova/internal/pacman"
)

// PacmanAdapter wraps a pacman/v1alpha1 caller and translates its wire calls to
// and from cymonkey/v1alpha2 engine profile messages.
type PacmanAdapter struct {
	caller bridge.Caller

	mu           sync.Mutex
	engine       string
	lastRevision string
}

func NewPacmanAdapter(caller bridge.Caller) *PacmanAdapter {
	return &PacmanAdapter{caller: caller}
}

func (a *PacmanAdapter) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if a.caller == nil {
		return nil, errors.New("underlying pacman caller is nil")
	}
	switch method {
	case bridge.MethodHello:
		return a.handleHello(ctx, params)
	case bridge.MethodCapabilities:
		return a.handleCapabilities(ctx, params)
	case bridge.MethodDescribe:
		return a.handleDescribe(ctx, params)
	case bridge.MethodAct:
		return a.handleAct(ctx, params)
	case bridge.MethodEvents:
		return a.caller.Call(ctx, method, params)
	case "health":
		return a.caller.Call(ctx, method, params)
	default:
		return nil, fmt.Errorf("unsupported Pacman adapter method %q", method)
	}
}

func (a *PacmanAdapter) handleHello(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	raw, err := a.caller.Call(ctx, bridge.MethodHello, params)
	if err != nil {
		return nil, err
	}
	var hello pacman.Hello
	if err := json.Unmarshal(raw, &hello); err != nil {
		return nil, fmt.Errorf("decode pacman hello response: %w", err)
	}
	a.mu.Lock()
	a.engine = hello.Implementation.Engine
	a.mu.Unlock()

	backend := Backend("engine-" + strings.ToLower(hello.Implementation.Engine))
	if hello.Implementation.Engine == "" {
		backend = BackendEnginePacman
	}

	response := Hello{
		ProtocolVersion:     ProtocolVersion,
		CompatibleProtocols: []string{pacman.ProtocolVersion},
		Implementation: Implementation{
			Name:    hello.Implementation.Name,
			Version: hello.Implementation.Version,
		},
		Profiles: []Profile{ProfileEngine},
		Backends: []Backend{backend},
		Features: hello.Features,
	}
	return json.Marshal(response)
}

func (a *PacmanAdapter) handleCapabilities(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	raw, err := a.caller.Call(ctx, bridge.MethodCapabilities, params)
	if err != nil {
		return nil, err
	}
	var caps []pacman.Capability
	if err := json.Unmarshal(raw, &caps); err != nil {
		return nil, fmt.Errorf("decode pacman capabilities response: %w", err)
	}

	a.mu.Lock()
	engineName := a.engine
	a.mu.Unlock()

	backend := Backend("engine-" + strings.ToLower(engineName))
	if engineName == "" {
		backend = BackendEnginePacman
	}

	result := make([]Capability, 0, len(caps))
	for _, item := range caps {
		targetKinds := make([]string, len(item.TargetKinds))
		for i, k := range item.TargetKinds {
			targetKinds[i] = string(k)
		}
		result = append(result, Capability{
			Name:        item.Name,
			Description: item.Description,
			Profile:     ProfileEngine,
			Backend:     backend,
			Support:     SupportNative,
			Lifetime:    LifetimeAttachment,
			Persistence: PersistenceSession,
			Effect:      item.Effect,
			TargetKinds: targetKinds,
			InputSchema: item.InputSchema,
		})
	}
	return json.Marshal(result)
}

func (a *PacmanAdapter) handleDescribe(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	raw, err := a.caller.Call(ctx, bridge.MethodDescribe, params)
	if err != nil {
		return nil, err
	}
	var desc pacman.Description
	if err := json.Unmarshal(raw, &desc); err != nil {
		return nil, fmt.Errorf("decode pacman describe response: %w", err)
	}

	a.mu.Lock()
	a.lastRevision = desc.Revision
	a.mu.Unlock()

	surfaces := make([]Surface, 0, len(desc.Resources))
	for _, res := range desc.Resources {
		surfaces = append(surfaces, Surface{
			ID:         res.ID,
			Profile:    ProfileEngine,
			Kind:       string(res.Kind),
			Label:      res.Label,
			Properties: res.Properties,
		})
	}

	result := Description{
		Revision:      desc.Revision,
		Surfaces:      surfaces,
		Augmentations: []AugmentationSummary{},
	}
	return json.Marshal(result)
}

func (a *PacmanAdapter) handleAct(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var payload struct {
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return nil, fmt.Errorf("decode cymonkey act request: %w", err)
	}

	a.mu.Lock()
	currentRev := a.lastRevision
	a.mu.Unlock()

	var expectedRev string
	if exp, ok := payload.Input["expectedRevision"].(string); ok {
		expectedRev = exp
	} else if exp, ok := payload.Input["revision"].(string); ok {
		expectedRev = exp
	}

	if expectedRev != "" && currentRev != "" && expectedRev != currentRev {
		return nil, fmt.Errorf("stale revision: expected %s, got %s", expectedRev, currentRev)
	}

	targetID := ""
	if tid, ok := payload.Input["targetId"].(string); ok {
		targetID = tid
	} else if tid, ok := payload.Input["target_id"].(string); ok {
		targetID = tid
	}

	inputBytes, _ := json.Marshal(payload.Input)
	pacmanReq := pacman.ActionRequest{
		Name:     payload.Name,
		TargetID: targetID,
		Input:    inputBytes,
	}

	pacmanParams, err := json.Marshal(pacmanReq)
	if err != nil {
		return nil, fmt.Errorf("encode pacman action request: %w", err)
	}

	return a.caller.Call(ctx, bridge.MethodAct, pacmanParams)
}
