package devicekit

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

var ErrChannelRequired = errors.New("board action requires a duplex channel")
var ErrLimit = errors.New("board stream limit reached")

// ChannelLimits are hard per-channel budgets. Zero disables a direction. Both
// directions cannot be disabled. The lifetime is capped at one hour.
type ChannelLimits struct {
	ReadBytes  int64         `json:"readBytes"`
	WriteBytes int64         `json:"writeBytes"`
	Duration   time.Duration `json:"duration"`
}

func (l ChannelLimits) Validate() error {
	if l.ReadBytes < 0 || l.WriteBytes < 0 || l.ReadBytes == 0 && l.WriteBytes == 0 || l.ReadBytes > 1<<40 || l.WriteBytes > 1<<40 || l.Duration <= 0 || l.Duration > time.Hour {
		return errors.New("channel needs byte budgets (up to 1 TiB each) and a duration up to one hour")
	}
	return nil
}

// DuplexSession opens one continuous byte stream. Close must unblock Read and
// Write. Providers bind the channel to the session's selected device.
type DuplexSession interface {
	Session
	OpenChannel(context.Context, Action, ChannelLimits) (io.ReadWriteCloser, error)
}

type attachment struct {
	conn       *Connection
	closer     io.Closer
	done       func()
	once       sync.Once
	err        error
	capability Capability
}

func (a *attachment) Close() error {
	a.once.Do(func() {
		a.done()
		a.err = a.closer.Close()
		a.conn.mu.Lock()
		delete(a.conn.attachments, a)
		a.conn.mu.Unlock()
		a.conn.events.add(Event{Type: "stream.closed", Capability: a.capability})
	})
	return a.err
}
func newConnection(session Session, allowed map[Capability]struct{}, resources map[string]struct{}, required map[Capability]bool) *Connection {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Connection{session: session, allowed: allowed, resources: resources, required: required, lifetime: ctx, cancel: cancel, attachments: map[*attachment]struct{}{}}
	c.events.add(Event{Type: "session.opened"})
	if source, ok := session.(EventSource); ok {
		events := source.Events()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case event, open := <-events:
					if !open {
						return
					}
					c.events.add(event)
				}
			}
		}()
	}
	return c
}
func (c *Connection) begin(ctx context.Context, action Action) (Session, context.Context, func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.authorize(action); err != nil {
		return nil, nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.lifetime, cancel)
	return c.session, callCtx, func() { stop(); cancel() }, nil
}
func (c *Connection) attach(ctx context.Context, closer io.Closer, done func(), capability Capability) (*attachment, error) {
	a := &attachment{conn: c, closer: closer, done: done, capability: capability}
	c.mu.Lock()
	if c.closed || ctx.Err() != nil || len(c.attachments) >= 32 {
		failure := ErrClosed
		if len(c.attachments) >= 32 {
			failure = ErrLimit
		}
		c.mu.Unlock()
		done()
		_ = closer.Close()
		return nil, failure
	}
	c.attachments[a] = struct{}{}
	c.mu.Unlock()
	stop := context.AfterFunc(ctx, func() { _ = a.Close() })
	// The stop function itself need not be retained: done cancels ctx and the
	// callback's idempotent Close releases the registration.
	_ = stop
	c.events.add(Event{Type: "stream.opened", Capability: capability})
	return a, nil
}
func (c *Connection) OpenChannel(ctx context.Context, action Action, limits ChannelLimits) (io.ReadWriteCloser, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	session, callCtx, done, err := c.begin(ctx, action)
	if err != nil {
		return nil, err
	}
	duplex, ok := session.(DuplexSession)
	if !ok {
		done()
		return nil, ErrNotStreamable
	}
	streamCtx, cancel := context.WithTimeout(callCtx, limits.Duration)
	cleanup := func() { cancel(); done() }
	raw, err := duplex.OpenChannel(streamCtx, action, limits)
	if err != nil {
		cleanup()
		return nil, err
	}
	if raw == nil {
		cleanup()
		return nil, errors.New("provider returned nil channel")
	}
	a, err := c.attach(streamCtx, raw, cleanup, action.Capability)
	if err != nil {
		return nil, err
	}
	return &limitedChannel{raw: raw, a: a, ctx: streamCtx, read: limits.ReadBytes, write: limits.WriteBytes}, nil
}

type limitedChannel struct {
	raw             io.ReadWriteCloser
	a               *attachment
	ctx             context.Context
	readMu, writeMu sync.Mutex
	read, write     int64
}

func (c *limitedChannel) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if c.read == 0 {
		return 0, ErrLimit
	}
	if int64(len(p)) > c.read {
		p = p[:int(c.read)]
	}
	n, err := c.raw.Read(p)
	c.read -= int64(n)
	if err != nil {
		_ = c.Close()
	}
	return n, err
}
func (c *limitedChannel) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > c.write {
		return 0, ErrLimit
	}
	n, err := c.raw.Write(p)
	c.write -= int64(n)
	if err != nil {
		_ = c.Close()
	}
	return n, err
}
func (c *limitedChannel) Close() error { return c.a.Close() }
