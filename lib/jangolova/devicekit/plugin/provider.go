// Package deviceplugin adapts an installed executable to Board's provider
// contract. Board's registry remains the grant gate for every action.
package deviceplugin

import (
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"time"

	extplugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	"jangolova/devicekit"
)

// Provider is one device provider served by an installed package. A package
// may serve several providers; Inner selects one by the provider ID it reports,
// and an empty Inner means the package as a whole under its installed name.
// Settings are host-chosen, non-secret launch parameters sent with list and open.
type Provider struct {
	Installed    store.Installed
	MethodPrefix string
	Inner        string
	Settings     map[string]string
}

const ContractName = "device.provider"
const ContractVersion = "v1"

// Operations lists every method a Board device provider package serves.
//
// events is additive to v1: a package that declares it reports the session's
// lifecycle events, and one that does not is served exactly as before.
var Operations = []string{"list", "open", "invoke", "stream.open", "stream.read", "stream.close", "close", "events"}

// eventPollInterval is how often the adapter asks a session for new events.
const eventPollInterval = 250 * time.Millisecond

// declaresEvents reports whether a package's descriptor serves the optional
// events operation under the given contract name.
func declaresEvents(descriptor extplugin.Descriptor, contract string) bool {
	if contract == "" {
		contract = ContractName
	}
	for _, c := range descriptor.Contracts {
		if c.Name != contract || c.Version != ContractVersion {
			continue
		}
		for _, operation := range c.Operations {
			if operation.Name == "events" {
				return true
			}
		}
	}
	return false
}

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

func (p Provider) ID() string {
	if p.Inner != "" {
		return p.Inner
	}
	return p.Installed.Name()
}

// params builds the optional selector and settings sent with list and open.
func (p Provider) params(extra map[string]any) map[string]any {
	if p.Inner == "" && len(p.Settings) == 0 && extra == nil {
		return nil
	}
	params := map[string]any{}
	for key, value := range extra {
		params[key] = value
	}
	if p.Inner != "" {
		params["provider"] = p.Inner
	}
	if len(p.Settings) != 0 {
		params["settings"] = p.Settings
	}
	return params
}

// Discover starts the package once, lists every device it serves and returns one
// Provider per provider ID the devices report, so a package that serves several
// providers keeps each under its own ID. A package whose devices carry no
// provider ID is returned as a single Provider named after the package.
func Discover(ctx context.Context, installed store.Installed, settings map[string]string) ([]Provider, error) {
	whole := Provider{Installed: installed, Settings: settings}
	devices, err := whole.List(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var providers []Provider
	for _, device := range devices {
		if device.ProviderID == "" || seen[device.ProviderID] {
			continue
		}
		seen[device.ProviderID] = true
		providers = append(providers, Provider{Installed: installed, Inner: device.ProviderID, Settings: settings})
	}
	if len(providers) == 0 {
		return []Provider{whole}, nil
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Inner < providers[j].Inner })
	return providers, nil
}

func (p Provider) List(ctx context.Context) ([]devicekit.Device, error) {
	proc, err := host.Open(ctx, p.Installed, host.Options{Authorize: listPolicy()})
	if err != nil {
		return nil, err
	}
	defer proc.Close()
	var devices []devicekit.Device
	if err := proc.Call(ctx, p.method("list"), p.params(nil), &devices); err != nil {
		return nil, err
	}
	return devices, nil
}

func (p Provider) Open(ctx context.Context, deviceID string, grant devicekit.Grant) (devicekit.Session, error) {
	proc, err := host.Open(ctx, p.Installed, host.Options{Authorize: sessionPolicy(grant)})
	if err != nil {
		return nil, err
	}
	if err := proc.Call(ctx, p.method("open"), p.params(map[string]any{"deviceId": deviceID, "grant": grant}), nil); err != nil {
		_ = proc.Close()
		return nil, err
	}
	s := &session{proc: proc, methodPrefix: p.MethodPrefix}
	if declaresEvents(proc.Descriptor(), p.MethodPrefix) {
		s.startEvents()
	}
	return s, nil
}

// Live serves the viewer half of a package whose process the caller already
// runs and owns. Every session opens against that one process and closing a
// session leaves the process running; the caller closes it. The process's
// admission policy must admit the device operations (see ViewerPolicy).
// MethodPrefix names the contract the package serves them under.
func Live(proc *host.Process, id, methodPrefix string) devicekit.Provider {
	return live{proc: proc, id: id, prefix: methodPrefix}
}

type live struct {
	proc   *host.Process
	id     string
	prefix string
}

func (l live) ID() string { return l.id }

func (l live) List(ctx context.Context) ([]devicekit.Device, error) {
	var devices []devicekit.Device
	if err := l.proc.Call(ctx, Provider{MethodPrefix: l.prefix}.method("list"), nil, &devices); err != nil {
		return nil, err
	}
	return devices, nil
}

func (l live) Open(ctx context.Context, deviceID string, grant devicekit.Grant) (devicekit.Session, error) {
	method := Provider{MethodPrefix: l.prefix}.method("open")
	if err := l.proc.Call(ctx, method, map[string]any{"deviceId": deviceID, "grant": grant}, nil); err != nil {
		return nil, err
	}
	s := &session{proc: l.proc, methodPrefix: l.prefix, shared: true}
	if declaresEvents(l.proc.Descriptor(), l.prefix) {
		s.startEvents()
	}
	return s, nil
}

type session struct {
	proc         *host.Process
	methodPrefix string
	mu           sync.Mutex
	closed       bool
	streams      map[*content]struct{}
	shared       bool // the process belongs to the caller; Close leaves it running
	events       chan devicekit.Event
	stopEvents   chan struct{}
	eventsDone   sync.WaitGroup
}

// Events implements devicekit.EventSource when the package reports events.
// The channel is nil otherwise, and a nil channel never delivers.
func (s *session) Events() <-chan devicekit.Event { return s.events }

func (s *session) startEvents() {
	s.events = make(chan devicekit.Event, 64)
	s.stopEvents = make(chan struct{})
	s.eventsDone.Add(1)
	go s.pollEvents()
}

func (s *session) pollEvents() {
	defer s.eventsDone.Done()
	defer close(s.events)
	ticker := time.NewTicker(eventPollInterval)
	defer ticker.Stop()
	var cursor uint64
	for {
		select {
		case <-s.stopEvents:
			return
		case <-ticker.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var batch devicekit.EventBatch
		err := s.proc.Call(ctx, s.method("events"), map[string]any{"after": cursor}, &batch)
		cancel()
		if err != nil {
			return
		}
		cursor = batch.Cursor
		for _, event := range batch.Events {
			select {
			case s.events <- event:
			case <-s.stopEvents:
				return
			}
		}
	}
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
	if s.stopEvents != nil {
		close(s.stopEvents)
	}
	streams := make([]*content, 0, len(s.streams))
	for c := range s.streams {
		streams = append(streams, c)
	}
	s.mu.Unlock()
	for _, c := range streams {
		_ = c.Close()
	}
	s.eventsDone.Wait()
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := s.proc.Call(callCtx, s.method("close"), nil, nil)
	if s.shared {
		return err
	}
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
