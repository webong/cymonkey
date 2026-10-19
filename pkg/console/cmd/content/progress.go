package main

import (
	"bytes"
	"errors"
	"sync"
	"unicode/utf8"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
)

// maxEventText is the longest text one progress event may carry.
const maxEventText = plugin.MaxProgressTextBytes

// logStream turns what a command writes to a stream into log events, one line
// per event. The host rate limits events, so a stream that writes faster than
// the limit loses lines; the host is told how many. When the host did not ask
// for progress (or cannot receive it), the text is kept so it can be returned
// with the final result instead.
type logStream struct {
	report func(plugin.Progress) error
	stream string

	mu      sync.Mutex
	partial []byte
	kept    bytes.Buffer
	over    bool
	off     bool // events cannot be delivered; keep the text
}

func newLogStream(report func(plugin.Progress) error, stream string) *logStream {
	return &logStream{report: report, stream: stream}
}

func (l *logStream) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	written := len(data)
	l.partial = append(l.partial, data...)
	for {
		newline := bytes.IndexByte(l.partial, '\n')
		if newline < 0 {
			break
		}
		l.emit(l.partial[:newline])
		l.partial = l.partial[newline+1:]
	}
	// A line that never ends must not grow without bound.
	if len(l.partial) > 16*maxEventText {
		l.emit(l.partial)
		l.partial = nil
	}
	return written, nil
}

// flush sends a final line that had no newline.
func (l *logStream) flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.partial) > 0 {
		l.emit(l.partial)
		l.partial = nil
	}
}

// emit reports one line, cut into events at character boundaries.
func (l *logStream) emit(line []byte) {
	for _, text := range splitLine(line) {
		if l.off {
			l.keep(text + "\n")
			continue
		}
		err := l.report(plugin.Progress{Kind: "log", Stream: l.stream, Text: text})
		switch {
		case err == nil, errors.Is(err, plugin.ErrProgressDropped):
		case errors.Is(err, plugin.ErrUnsupported):
			l.off = true
			l.keep(text + "\n")
		default:
			// The call is ending or the host stopped listening; keep the text
			// for the final result.
			l.off = true
			l.keep(text + "\n")
		}
	}
}

func (l *logStream) keep(text string) {
	room := maxKept - l.kept.Len()
	if len(text) > room {
		l.over = true
		text = text[:max(room, 0)]
	}
	l.kept.WriteString(text)
}

// returned is the text that was not delivered as events.
func (l *logStream) returned() []byte { return l.kept.Bytes() }

// splitLine cuts a line into pieces of at most maxEventText bytes without
// splitting a UTF-8 character, and replaces invalid bytes so each piece is
// valid event text.
func splitLine(line []byte) []string {
	if len(line) == 0 {
		return []string{""}
	}
	var pieces []string
	for len(line) > 0 {
		take := min(len(line), maxEventText)
		for take < len(line) && take > 0 && !utf8.RuneStart(line[take]) {
			take--
		}
		if take == 0 {
			take = min(len(line), maxEventText)
		}
		pieces = append(pieces, string(bytes.ToValidUTF8(line[:take], []byte("�"))))
		line = line[take:]
	}
	return pieces
}
