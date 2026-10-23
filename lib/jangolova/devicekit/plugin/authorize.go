package deviceplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	extplugin "github.com/webong/ext/pkg/plugin-go/contract"
	"jangolova/devicekit"
)

// sequence is an admission policy for one plugin process. ext asks it before
// every call, so a process can do only what its role allows, in the order the
// protocol defines. It does not replace the host's own approvals; it stops a
// bug above this layer from reaching an operation the process should never see.
type sequence struct {
	mu     sync.Mutex
	first  string          // the one operation that must come first, and only once
	opened bool            // first has been admitted
	after  map[string]bool // operations admitted once first has run
	check  func(extplugin.Request) error
}

func (s *sequence) authorize(_ context.Context, request extplugin.Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	operation := request.Operation
	if s.first != "" && operation == s.first {
		if s.opened {
			return fmt.Errorf("plugin operation %q is allowed once per process", operation)
		}
		s.opened = true
		return nil
	}
	if s.first != "" && !s.opened {
		return fmt.Errorf("plugin operation %q must follow %q", operation, s.first)
	}
	if !s.after[operation] {
		return fmt.Errorf("plugin operation %q is not allowed here", operation)
	}
	if s.check != nil {
		return s.check(request)
	}
	return nil
}

// listPolicy admits only list, for the short-lived process that enumerates
// devices.
func listPolicy() func(context.Context, extplugin.Request) error {
	s := &sequence{after: map[string]bool{"list": true}}
	return s.authorize
}

// sessionPolicy admits open once, first, then the operations of a device
// session. invoke and stream.open must name a capability the grant allows and,
// when they name a resource, one the grant covers. Board's connection layer
// applies the same rule before it calls the session; this repeats it inside the
// plugin process's own admission check.
func sessionPolicy(grant devicekit.Grant) func(context.Context, extplugin.Request) error {
	s := &sequence{first: "open", after: map[string]bool{
		"invoke": true, "stream.open": true, "stream.read": true, "stream.close": true, "close": true, "events": true,
	}}
	s.check = grantCheck(grant)
	return s.authorize
}

// grantCheck admits invoke and stream.open only for a capability the grant
// allows and, when the action names a resource, one the grant covers. Other
// operations pass.
func grantCheck(grant devicekit.Grant) func(extplugin.Request) error {
	capabilities := make(map[devicekit.Capability]bool, len(grant.Capabilities))
	for _, capability := range grant.Capabilities {
		capabilities[capability] = true
	}
	resources := make(map[string]bool, len(grant.ResourceIDs))
	for _, resource := range grant.ResourceIDs {
		resources[resource] = true
	}
	return func(request extplugin.Request) error {
		if request.Operation != "invoke" && request.Operation != "stream.open" {
			return nil
		}
		var action devicekit.Action
		if err := json.Unmarshal(request.Payload, &action); err != nil {
			return fmt.Errorf("%w: unreadable action", devicekit.ErrNotGranted)
		}
		if !capabilities[action.Capability] {
			return fmt.Errorf("%w: capability %q", devicekit.ErrNotGranted, action.Capability)
		}
		if action.ResourceID != "" && !resources[action.ResourceID] {
			return fmt.Errorf("%w: resource %q", devicekit.ErrNotGranted, action.ResourceID)
		}
		return nil
	}
}

// ViewerPolicy is the admission policy for a long-lived process whose viewer
// sessions come and go: list is always admitted, open at any time (the process
// itself refuses a second concurrent session), and invoke and stream.open only
// for what the most recent open's grant allows, and only while that session is
// open. The caller composes it with the policy for the operations of its own
// contract.
func ViewerPolicy() func(context.Context, extplugin.Request) error {
	p := &viewerPolicy{}
	return p.authorize
}

type viewerPolicy struct {
	mu         sync.Mutex
	open       bool
	checkGrant func(extplugin.Request) error
}

func (p *viewerPolicy) authorize(_ context.Context, request extplugin.Request) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch request.Operation {
	case "list", "events":
		return nil
	case "open":
		var params struct {
			Grant devicekit.Grant `json:"grant"`
		}
		if err := json.Unmarshal(request.Payload, &params); err != nil {
			return fmt.Errorf("%w: unreadable grant", devicekit.ErrNotGranted)
		}
		p.open = true
		p.checkGrant = grantCheck(params.Grant)
		return nil
	case "close":
		p.open = false
		p.checkGrant = nil
		return nil
	case "stream.read", "stream.close":
		if !p.open {
			return fmt.Errorf("plugin operation %q needs an open session", request.Operation)
		}
		return nil
	case "invoke", "stream.open":
		if !p.open {
			return fmt.Errorf("plugin operation %q needs an open session", request.Operation)
		}
		return p.checkGrant(request)
	}
	return fmt.Errorf("plugin operation %q is not allowed here", request.Operation)
}
