// Package device builds explicit Board providers from device-bound operations.
package device

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"kit/devicekit"
	"sort"
	"sync"
	"time"
)

type Operation struct {
	Invoke  func(context.Context, devicekit.Action) (devicekit.Result, error)
	Stream  func(context.Context, devicekit.Action) (devicekit.Content, error)
	Channel func(context.Context, devicekit.Action, devicekit.ChannelLimits) (io.ReadWriteCloser, error)
}
type Binding struct {
	Operations map[devicekit.Capability]Operation
	Close      func(context.Context) error
	Events     <-chan devicekit.Event
	Health     func(context.Context) (bool, error)
}
type Provider struct {
	ProviderID  string
	Device      devicekit.Device
	ResourceIDs []string
	Bind        func(context.Context, devicekit.Grant) (Binding, error)
}

func (p *Provider) ID() string { return p.ProviderID }
func (p *Provider) List(ctx context.Context) ([]devicekit.Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d := p.Device
	d.ProviderID = p.ProviderID
	d.Capabilities = append([]devicekit.CapabilityDescriptor(nil), d.Capabilities...)
	for i := range d.Capabilities {
		d.Capabilities[i].InputSchema = append(json.RawMessage(nil), d.Capabilities[i].InputSchema...)
		d.Capabilities[i].OutputSchema = append(json.RawMessage(nil), d.Capabilities[i].OutputSchema...)
	}
	sort.Slice(d.Capabilities, func(i, j int) bool { return d.Capabilities[i].Name < d.Capabilities[j].Name })
	return []devicekit.Device{d}, nil
}
func (p *Provider) Open(ctx context.Context, id string, g devicekit.Grant) (devicekit.Session, error) {
	if p.Bind == nil || id != p.Device.ID {
		return nil, devicekit.ErrUnknownDevice
	}
	allowed := map[devicekit.Capability]bool{}
	resources := map[string]bool{}
	required := map[devicekit.Capability]bool{}
	if len(g.Capabilities) == 0 {
		return nil, devicekit.ErrNotGranted
	}
	for _, cap := range g.Capabilities {
		found := false
		for _, d := range p.Device.Capabilities {
			if d.Name == cap {
				found = true
				required[cap] = d.ResourceRequired
			}
		}
		if !found {
			return nil, devicekit.ErrNotGranted
		}
		allowed[cap] = true
		if required[cap] && len(g.ResourceIDs) == 0 {
			return nil, devicekit.ErrNotGranted
		}
	}
	for _, id := range g.ResourceIDs {
		found := false
		for _, published := range p.ResourceIDs {
			found = found || id == published
		}
		if !found {
			return nil, devicekit.ErrNotGranted
		}
		resources[id] = true
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(context.Background())
	binding, err := p.Bind(life, g)
	if err != nil {
		cancel()
		return nil, err
	}
	s := &session{binding: binding, allowed: allowed, resources: resources, required: required, life: life, cancel: cancel, streams: map[*tracked]bool{}, events: make(chan devicekit.Event, 64)}
	s.startEvents()
	return s, nil
}

type session struct {
	mu        sync.Mutex
	binding   Binding
	allowed   map[devicekit.Capability]bool
	resources map[string]bool
	required  map[devicekit.Capability]bool
	life      context.Context
	cancel    context.CancelFunc
	closed    bool
	streams   map[*tracked]bool
	events    chan devicekit.Event
}

func (s *session) Events() <-chan devicekit.Event { return s.events }
func (s *session) operation(ctx context.Context, a devicekit.Action) (Operation, context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Operation{}, nil, nil, devicekit.ErrClosed
	}
	if !s.allowed[a.Capability] || s.required[a.Capability] && a.ResourceID == "" || a.ResourceID != "" && !s.resources[a.ResourceID] {
		return Operation{}, nil, nil, devicekit.ErrNotGranted
	}
	op, ok := s.binding.Operations[a.Capability]
	if !ok {
		return Operation{}, nil, nil, devicekit.ErrNotGranted
	}
	if err := ctx.Err(); err != nil {
		return Operation{}, nil, nil, err
	}
	call, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.life, cancel)
	return op, call, func() { stop(); cancel() }, nil
}
func (s *session) Invoke(ctx context.Context, a devicekit.Action) (devicekit.Result, error) {
	op, call, done, err := s.operation(ctx, a)
	if err != nil {
		return devicekit.Result{}, err
	}
	defer done()
	if op.Invoke == nil {
		if op.Channel != nil {
			return devicekit.Result{}, devicekit.ErrChannelRequired
		}
		return devicekit.Result{}, devicekit.ErrStreamRequired
	}
	return op.Invoke(call, a)
}
func (s *session) Stream(ctx context.Context, a devicekit.Action) (devicekit.Content, error) {
	op, call, done, err := s.operation(ctx, a)
	if err != nil {
		return nil, err
	}
	if op.Stream == nil {
		done()
		return nil, devicekit.ErrNotStreamable
	}
	raw, err := op.Stream(call, a)
	if err != nil {
		done()
		return nil, err
	}
	if raw == nil {
		done()
		return nil, errors.New("nil device content")
	}
	t, err := s.track(call, raw, done)
	if err != nil {
		return nil, err
	}
	return &content{Content: raw, tracked: t}, nil
}
func (s *session) OpenChannel(ctx context.Context, a devicekit.Action, l devicekit.ChannelLimits) (io.ReadWriteCloser, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	op, call, done, err := s.operation(ctx, a)
	if err != nil {
		return nil, err
	}
	if op.Channel == nil {
		done()
		return nil, devicekit.ErrNotStreamable
	}
	raw, err := op.Channel(call, a, l)
	if err != nil {
		done()
		return nil, err
	}
	if raw == nil {
		done()
		return nil, errors.New("nil device channel")
	}
	t, err := s.track(call, raw, done)
	if err != nil {
		return nil, err
	}
	return &channel{ReadWriteCloser: raw, tracked: t}, nil
}

type tracked struct {
	s    *session
	raw  io.Closer
	done func()
	once sync.Once
	err  error
}

func (t *tracked) Close() error {
	t.once.Do(func() { t.err = t.raw.Close(); t.done(); t.s.mu.Lock(); delete(t.s.streams, t); t.s.mu.Unlock() })
	return t.err
}
func (s *session) track(ctx context.Context, raw io.Closer, done func()) (*tracked, error) {
	t := &tracked{s: s, raw: raw, done: done}
	s.mu.Lock()
	if s.closed || ctx.Err() != nil || len(s.streams) >= 32 {
		failure := devicekit.ErrClosed
		if len(s.streams) >= 32 {
			failure = devicekit.ErrLimit
		}
		s.mu.Unlock()
		done()
		_ = raw.Close()
		return nil, failure
	}
	s.streams[t] = true
	s.mu.Unlock()
	context.AfterFunc(ctx, func() { _ = t.Close() })
	return t, nil
}

type content struct {
	devicekit.Content
	*tracked
}

func (c *content) Close() error { return c.tracked.Close() }

type channel struct {
	io.ReadWriteCloser
	*tracked
}

func (c *channel) Close() error { return c.tracked.Close() }
func (s *session) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.cancel()
	all := make([]*tracked, 0, len(s.streams))
	for t := range s.streams {
		all = append(all, t)
	}
	s.mu.Unlock()
	var err error
	for _, t := range all {
		err = errors.Join(err, t.Close())
	}
	if s.binding.Close != nil {
		err = errors.Join(err, s.binding.Close(ctx))
	}
	return err
}

// Decode rejects unknown fields, trailing JSON, and oversized action metadata.
func Decode(raw json.RawMessage, value any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > 64<<10 {
		return errors.New("device input exceeds 64 KiB")
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return errors.New("device input must be an object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("device input contains trailing JSON")
	}
	return nil
}
func Result(value any) (devicekit.Result, error) {
	b, err := json.Marshal(value)
	return devicekit.Result{Output: b}, err
}
func Empty(raw json.RawMessage) error { return Decode(raw, &struct{}{}) }
func Descriptor(cap devicekit.Capability, schema string, resource bool) devicekit.CapabilityDescriptor {
	return devicekit.CapabilityDescriptor{Name: cap, InputSchema: json.RawMessage(schema), ResourceRequired: resource}
}

const EmptySchema = `{"type":"object","additionalProperties":false}`

// Bytes returns finite content without mixing resource bytes into action JSON.
func Bytes(b []byte) devicekit.Content {
	return &bytesContent{reader: bytes.NewReader(b), size: int64(len(b))}
}

type bytesContent struct {
	reader *bytes.Reader
	mu     sync.Mutex
	size   int64
}

func (b *bytesContent) Size() int64   { return b.size }
func (*bytesContent) Truncated() bool { return false }
func (b *bytesContent) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reader.Read(p)
}
func (b *bytesContent) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reader.Reset(nil)
	return nil
}

// Health probes run only under an explicit health grant. Event payloads never
// contain paths, captured media, signaling material, or typed input.
func (s *session) startEvents() {
	go func() {
		for {
			select {
			case <-s.life.Done():
				return
			case event, ok := <-s.binding.Events:
				if !ok {
					return
				}
				s.emit(event)
			}
		}
	}()
	if s.binding.Health == nil || !s.allowed[devicekit.DeviceHealth] {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		first := true
		previous := false
		for {
			ctx, cancel := context.WithTimeout(s.life, 3*time.Second)
			ready, err := s.binding.Health(ctx)
			cancel()
			ready = ready && err == nil
			if first || ready != previous {
				kind := "device.unavailable"
				if ready {
					kind = "device.available"
				}
				s.emit(devicekit.Event{Type: kind, Capability: devicekit.DeviceHealth})
				first = false
				previous = ready
			}
			select {
			case <-s.life.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *session) emit(e devicekit.Event) {
	select {
	case s.events <- e:
	default:
	}
}
