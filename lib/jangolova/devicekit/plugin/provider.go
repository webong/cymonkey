// Package deviceplugin adapts an installed executable to Board's provider
// contract. Board's registry remains the grant gate for every action.
package deviceplugin

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/webong/ext/pkg/plugin-go/contract/host"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	"jangolova/devicekit"
)

type Provider struct {
	Installed    store.Installed
	MethodPrefix string
}

const ContractName = "device.provider"
const ContractVersion = "v1"

// Operations lists every method a Board device provider package serves.
var Operations = []string{"list", "open", "invoke", "stream.open", "stream.read", "stream.close", "close"}

func (p Provider) method(name string) string {
	prefix := p.MethodPrefix
	if prefix == "" {
		prefix = ContractName
	}
	return prefix + "." + name
}

// Kind is retained for source compatibility with the original manifest field.
// Deprecated: use ContractName.
const Kind = ContractName

// Providers selects Board executables from a shared installation inventory.
func Providers(installed []store.Installed) []devicekit.Provider {
	var providers []devicekit.Provider
	for _, item := range installed {
		if item.Supports(ContractName, ContractVersion) {
			providers = append(providers, Provider{Installed: item})
		}
	}
	return providers
}

func (p Provider) ID() string { return p.Installed.Name() }

func (p Provider) List(ctx context.Context) ([]devicekit.Device, error) {
	proc, err := host.Open(ctx, p.Installed, host.Options{Authorize: listPolicy()})
	if err != nil {
		return nil, err
	}
	defer proc.Close()
	var devices []devicekit.Device
	if err := proc.Call(ctx, p.method("list"), nil, &devices); err != nil {
		return nil, err
	}
	return devices, nil
}

func (p Provider) Open(ctx context.Context, deviceID string, grant devicekit.Grant) (devicekit.Session, error) {
	proc, err := host.Open(ctx, p.Installed, host.Options{Authorize: sessionPolicy(grant)})
	if err != nil {
		return nil, err
	}
	if err := proc.Call(ctx, p.method("open"), map[string]any{"deviceId": deviceID, "grant": grant}, nil); err != nil {
		_ = proc.Close()
		return nil, err
	}
	return &session{proc: proc, methodPrefix: p.MethodPrefix}, nil
}

type session struct {
	proc         *host.Process
	methodPrefix string
	mu           sync.Mutex
	closed       bool
	streams      map[*content]struct{}
}

func (s *session) method(name string) string {
	prefix := s.methodPrefix
	if prefix == "" {
		prefix = ContractName
	}
	return prefix + "." + name
}

func (s *session) Invoke(ctx context.Context, action devicekit.Action) (devicekit.Result, error) {
	if action.Capability == devicekit.DriveRead {
		return devicekit.Result{}, devicekit.ErrStreamRequired
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return devicekit.Result{}, devicekit.ErrClosed
	}
	var result devicekit.Result
	if err := s.proc.Call(ctx, s.method("invoke"), action, &result); err != nil {
		return devicekit.Result{}, err
	}
	return result, nil
}

func (s *session) Stream(ctx context.Context, action devicekit.Action) (devicekit.Content, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, devicekit.ErrClosed
	}
	var result struct {
		ID        string `json:"id"`
		Size      int64  `json:"size"`
		Truncated bool   `json:"truncated"`
	}
	if err := s.proc.Call(ctx, s.method("stream.open"), action, &result); err != nil {
		return nil, err
	}
	if result.ID == "" || result.Size < 0 || result.Size > 128<<20 {
		return nil, errors.New("board plugin returned invalid stream metadata")
	}
	c := &content{session: s, ctx: ctx, id: result.ID, size: result.Size, truncated: result.Truncated}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = c.Close()
		return nil, devicekit.ErrClosed
	}
	if s.streams == nil {
		s.streams = make(map[*content]struct{})
	}
	s.streams[c] = struct{}{}
	s.mu.Unlock()
	return c, nil
}

func (s *session) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	streams := make([]*content, 0, len(s.streams))
	for c := range s.streams {
		streams = append(streams, c)
	}
	s.mu.Unlock()
	for _, c := range streams {
		_ = c.Close()
	}
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = s.proc.Call(callCtx, s.method("close"), nil, nil)
	return s.proc.Close()
}

type content struct {
	session   *session
	ctx       context.Context
	id        string
	size      int64
	truncated bool
	mu        sync.Mutex
	pending   []byte
	read      int64
	eof       bool
	closed    bool
}

func (c *content) Size() int64     { return c.size }
func (c *content) Truncated() bool { return c.truncated }
func (c *content) Read(dst []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, devicekit.ErrClosed
	}
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	if len(dst) == 0 {
		return 0, nil
	}
	if len(c.pending) == 0 && !c.eof {
		var result struct {
			Data []byte `json:"data"`
			EOF  bool   `json:"eof"`
		}
		if err := c.session.proc.Call(c.ctx, c.session.method("stream.read"), map[string]any{"id": c.id, "maxBytes": 32768}, &result); err != nil {
			return 0, err
		}
		if len(result.Data) > 32768 || c.read+int64(len(result.Data)) > c.size {
			return 0, errors.New("board plugin exceeded stream bound")
		}
		c.pending = result.Data
		c.eof = result.EOF
		if len(c.pending) == 0 && !c.eof {
			return 0, errors.New("board plugin returned an empty nonterminal chunk")
		}
	}
	n := copy(dst, c.pending)
	c.pending = c.pending[n:]
	c.read += int64(n)
	if n == 0 && c.eof {
		return 0, io.EOF
	}
	return n, nil
}
func (c *content) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	c.session.mu.Lock()
	delete(c.session.streams, c)
	c.session.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return c.session.proc.Call(ctx, c.session.method("stream.close"), map[string]string{"id": c.id}, nil)
}
