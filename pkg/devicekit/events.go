package devicekit

import (
	"errors"
	"sync"
	"time"
)

var ErrCursorExpired = errors.New("board event cursor expired")

// Event contains lifecycle metadata only, never captured content or credentials.
type Event struct {
	Cursor     uint64     `json:"cursor"`
	Type       string     `json:"type"`
	Capability Capability `json:"capability,omitempty"`
	At         time.Time  `json:"at"`
}
type EventBatch struct {
	Events []Event `json:"events"`
	Cursor uint64  `json:"cursor"`
}

// EventSource optionally reports device or transport state changes. The session
// owns this channel and stops production on Close. Consumers also stop when
// their connection closes. Producers must bound buffering.
type EventSource interface{ Events() <-chan Event }

type eventLog struct {
	mu     sync.Mutex
	items  []Event
	cursor uint64
}

func (l *eventLog) add(event Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cursor++
	event.Cursor = l.cursor
	event.At = time.Now().UTC()
	if len(l.items) == 256 {
		copy(l.items, l.items[1:])
		l.items = l.items[:255]
	}
	l.items = append(l.items, event)
}
func (c *Connection) Events(after uint64) (EventBatch, error) {
	c.events.mu.Lock()
	defer c.events.mu.Unlock()
	result := EventBatch{Cursor: c.events.cursor, Events: []Event{}}
	if after > result.Cursor || (len(c.events.items) > 0 && after < c.events.items[0].Cursor-1) {
		return result, ErrCursorExpired
	}
	for _, event := range c.events.items {
		if event.Cursor > after {
			result.Events = append(result.Events, event)
		}
	}
	return result, nil
}
