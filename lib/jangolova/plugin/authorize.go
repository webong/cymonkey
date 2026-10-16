package jangolovaplugin

import (
	"context"
	"fmt"
	"sync"

	extplugin "github.com/webong/ext/pkg/plugin-go/contract"
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
	return nil
}

// probePolicy admits only inspect, for a short-lived availability check.
func probePolicy() func(context.Context, extplugin.Request) error {
	s := &sequence{after: map[string]bool{"inspect": true}}
	return s.authorize
}

// sessionPolicy admits connect once, first, then the operations of a live
// engine session.
func sessionPolicy() func(context.Context, extplugin.Request) error {
	s := &sequence{first: "connect", after: map[string]bool{
		"call": true, "authorize": true, "health": true, "disconnect": true, "events": true, "material": true,
	}}
	return s.authorize
}
