package blockadeplugin

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

// workerPolicy admits start once, first, then the operations of a started
// provider worker.
func workerPolicy() func(context.Context, extplugin.Request) error {
	s := &sequence{first: "start", after: map[string]bool{
		"observe": true, "interpret": true, "capabilities": true, "health": true, "close": true,
	}}
	return s.authorize
}

// inspectPolicy admits only model.inspect, for the short-lived process that
// checks a model before it is published.
func inspectPolicy() func(context.Context, extplugin.Request) error {
	s := &sequence{after: map[string]bool{"model.inspect": true}}
	return s.authorize
}
