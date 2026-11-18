package mobilekit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/endpoint"
	"github.com/webong/ext/pkg/plugin-go/contract/host"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	"jangolova/devicekit"
	deviceplugin "jangolova/devicekit/plugin"
)

var (
	// ErrConverterUnavailable means the package could not start because a tool
	// it needs (ffmpeg) is missing.
	ErrConverterUnavailable = errors.New("mobile target converter is unavailable")
	// ErrInvalidTarget means the package refused the target's options.
	ErrInvalidTarget = errors.New("mobile target options were refused")
)

const startTimeout = 15 * time.Second

// Target is a started mobile target as the host sees it: the package's
// process, the admitted ingest endpoint and the viewer provider.
type Target struct {
	proc  *host.Process
	grant *endpoint.Grant
	id    string
}

// Start launches the package, starts the target and admits its endpoint. The
// request's TTL bounds the endpoint's life.
func Start(ctx context.Context, installed store.Installed, request StartRequest) (*Target, error) {
	proc, err := host.Open(ctx, installed, host.Options{Authorize: policy()})
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	var raw struct {
		Endpoint json.RawMessage `json:"endpoint"`
	}
	if err := proc.Call(callCtx, Contract+".start", request, &raw); err != nil {
		_ = proc.Close()
		return nil, classify(err)
	}
	declared, err := endpoint.Decode(raw.Endpoint)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	lifetime := time.Duration(request.TTLSeconds)*time.Second + time.Minute
	grant, err := endpoint.Admit(proc, declared, endpoint.Policy{Schemes: []string{"ws"}, MaxLifetime: lifetime})
	if err != nil {
		_ = proc.Close()
		return nil, err
	}
	return &Target{proc: proc, grant: grant, id: request.ID}, nil
}

// classify turns a start refusal into a sentinel. The remote text is untrusted
// and may name configuration, so it is dropped.
func classify(err error) error {
	var remote *plugin.RemoteError
	if errors.As(err, &remote) {
		switch remote.Code {
		case CodeConverterUnavailable:
			return ErrConverterUnavailable
		case CodeInvalidTarget:
			return ErrInvalidTarget
		}
	}
	return err
}

// Done is closed when the package's process has ended.
func (t *Target) Done() <-chan struct{} { return t.proc.Done() }

// Provider serves the viewer half. Its sessions open and close against the
// target's own process.
func (t *Target) Provider() devicekit.Provider {
	return deviceplugin.Live(t.proc, t.id, Contract)
}

// Dial opens the connection that carries the app's frames to the target. The
// endpoint is a secret; no error from here names it.
func (t *Target) Dial(ctx context.Context) (*websocket.Conn, error) {
	scheme, address, path, err := t.grant.Target()
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	authorization, err := t.grant.AuthorizationHeader()
	if err != nil {
		return nil, err
	}
	if authorization != "" {
		header.Set("Authorization", authorization)
	}
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second}).DialContext(ctx, scheme+"://"+address+path, header)
	if err != nil {
		return nil, errors.New("mobile target ingest is unavailable")
	}
	return conn, nil
}

// Close stops the target and ends its process.
func (t *Target) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = t.proc.Call(ctx, Contract+".stop", nil, nil)
	t.grant.Revoke()
	return t.proc.Close()
}

// policy admits start first and once, stop once, and then the operations of
// the viewer half under deviceplugin.ViewerPolicy.
func policy() func(context.Context, plugin.Request) error {
	viewer := deviceplugin.ViewerPolicy()
	var mu sync.Mutex
	started, stopped := false, false
	return func(ctx context.Context, request plugin.Request) error {
		mu.Lock()
		defer mu.Unlock()
		switch request.Operation {
		case "start":
			if started {
				return fmt.Errorf("plugin operation %q is allowed once per process", request.Operation)
			}
			started = true
			return nil
		case "stop":
			if !started || stopped {
				return fmt.Errorf("plugin operation %q is not allowed here", request.Operation)
			}
			stopped = true
			return nil
		}
		if !started {
			return fmt.Errorf("plugin operation %q must follow %q", request.Operation, "start")
		}
		if stopped {
			return fmt.Errorf("plugin operation %q is not allowed after stop", request.Operation)
		}
		return viewer(ctx, request)
	}
}
