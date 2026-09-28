package board

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// ErrNotStreamable reports that an attached session cannot deliver bounded
// content for an action. A provider must implement StreamingSession before any
// resource bytes can reach a caller.
var ErrNotStreamable = errors.New("board session cannot stream content")

// ErrStreamRequired reports that an action produces resource content and must
// be delivered through Stream. Keeping file bytes out of Result.Output is what
// lets a host log, compare, and forward a result without also copying the
// contents it describes.
var ErrStreamRequired = errors.New("board action delivers content through Stream")

// Content is a bounded, forward-only stream of one resource's bytes. A stream
// is created for one granted action, is read at most once, and is released by
// Close. Closing the owning session closes any stream still open.
type Content interface {
	io.Reader
	// Size is the exact number of bytes Read can return in total. A provider
	// that refuses to stat a resource ahead of time reports a bound it
	// enforces instead, so Size is never larger than what the grant allows.
	Size() int64
	// Truncated reports that the resource holds more bytes than this stream
	// will deliver. A caller that needs the remainder must re-open it under a
	// new grant rather than expecting this stream to grow.
	Truncated() bool
	// Close releases the underlying attachment. It is safe to call more than
	// once.
	Close() error
}

// StreamingSession is the optional Session capability for actions whose result
// is too large, or too sensitive, to carry in a Result. Connection.Stream
// applies the same grant gate as Connection.Invoke before reaching a stream, so
// a provider never has to trust that a caller checked its own grant.
type StreamingSession interface {
	Session
	Stream(context.Context, Action) (Content, error)
}

// Stream opens a bounded content stream for one action. The grant is enforced
// first: an ungranted capability or resource fails here exactly as it would for
// Invoke, and no provider code runs.
func (c *Connection) Stream(ctx context.Context, action Action) (Content, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.authorize(action); err != nil {
		return nil, err
	}
	streamer, ok := c.session.(StreamingSession)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotStreamable, action.Capability)
	}
	return streamer.Stream(ctx, action)
}

// ReadAll drains a content stream into memory under an explicit ceiling. It
// exists so a host that genuinely needs bytes can say how many it will hold;
// the provider's own bound still applies first. A stream larger than limit
// yields the bytes read so far alongside the error.
func ReadAll(content Content, limit int64) ([]byte, error) {
	if content == nil {
		return nil, errors.New("board content is nil")
	}
	if limit <= 0 {
		return nil, errors.New("board content limit must be positive")
	}
	buffer := make([]byte, 0, 4096)
	chunk := make([]byte, 32*1024)
	for {
		read, err := content.Read(chunk)
		if read > 0 {
			keep := read
			exceeded := false
			if int64(len(buffer))+int64(read) > limit {
				keep = int(limit) - len(buffer)
				exceeded = true
			}
			buffer = append(buffer, chunk[:keep]...)
			if exceeded {
				return buffer, fmt.Errorf("board content exceeded the %d byte host limit", limit)
			}
		}
		if errors.Is(err, io.EOF) {
			return buffer, nil
		}
		if err != nil {
			return buffer, err
		}
	}
}
