package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
)

func collect(events *[]plugin.Progress, err error) func(plugin.Progress) error {
	return func(p plugin.Progress) error {
		*events = append(*events, p)
		return err
	}
}

func TestEachLineBecomesAnEventAndAnUnfinishedLineIsFlushed(t *testing.T) {
	var events []plugin.Progress
	stream := newLogStream(collect(&events, nil), "err")
	stream.Write([]byte("first\nsec"))
	stream.Write([]byte("ond\nthird"))
	stream.flush()
	var got []string
	for _, event := range events {
		if event.Kind != "log" || event.Stream != "err" {
			t.Fatalf("event = %+v", event)
		}
		got = append(got, event.Text)
	}
	if strings.Join(got, "|") != "first|second|third" {
		t.Fatalf("events = %v", got)
	}
	if len(stream.returned()) != 0 {
		t.Fatalf("delivered text was also kept: %q", stream.returned())
	}
}

func TestALongLineIsCutAtCharacterBoundariesIntoLegalEvents(t *testing.T) {
	var events []plugin.Progress
	stream := newLogStream(collect(&events, nil), "err")
	// Two-byte characters make a cut at exactly 2048 bytes fall mid-character
	// when the line starts with an odd byte.
	line := "x" + strings.Repeat("é", 3000) + "\n"
	stream.Write([]byte(line))
	var joined strings.Builder
	for _, event := range events {
		if len(event.Text) > maxEventText || !utf8.ValidString(event.Text) {
			t.Fatalf("illegal event: %d bytes, valid=%v", len(event.Text), utf8.ValidString(event.Text))
		}
		if err := event.Validate(); err != nil {
			t.Fatalf("event does not validate: %v", err)
		}
		joined.WriteString(event.Text)
	}
	if len(events) < 3 || joined.String() != strings.TrimSuffix(line, "\n") {
		t.Fatalf("%d events, text preserved=%v", len(events), joined.String() == strings.TrimSuffix(line, "\n"))
	}
}

func TestInvalidBytesAreReplacedSoTheEventIsLegal(t *testing.T) {
	var events []plugin.Progress
	stream := newLogStream(collect(&events, nil), "err")
	stream.Write([]byte("bad \xff byte\n"))
	if len(events) != 1 || !utf8.ValidString(events[0].Text) || !strings.Contains(events[0].Text, "�") {
		t.Fatalf("events = %+v", events)
	}
}

func TestWithoutProgressTheTextIsReturnedWithTheResult(t *testing.T) {
	var events []plugin.Progress
	stream := newLogStream(collect(&events, plugin.ErrUnsupported), "err")
	stream.Write([]byte("kept one\nkept two\n"))
	stream.flush()
	if string(stream.returned()) != "kept one\nkept two\n" {
		t.Fatalf("returned = %q", stream.returned())
	}
	if len(events) != 1 {
		t.Fatalf("reporting continued after the host said it cannot receive events: %d attempts", len(events))
	}
}

func TestADroppedEventIsNotAnError(t *testing.T) {
	var events []plugin.Progress
	stream := newLogStream(collect(&events, plugin.ErrProgressDropped), "err")
	stream.Write([]byte("a\nb\nc\n"))
	if len(events) != 3 || len(stream.returned()) != 0 {
		t.Fatalf("attempts=%d returned=%q", len(events), stream.returned())
	}
}
