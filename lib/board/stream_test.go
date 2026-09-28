package board

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// streamingProvider records whether a streaming action ever reached it, which
// is how the gate tests prove an ungranted action stops at the Connection.
type streamingProvider struct {
	session *streamingSession
}

func (p *streamingProvider) ID() string { return "streaming-test" }

func (p *streamingProvider) List(context.Context) ([]Device, error) {
	return []Device{{
		ID: "drive-1", Kind: "drive",
		Capabilities: []CapabilityDescriptor{capability(DriveList), capability(DriveRead)},
	}}, nil
}

func (p *streamingProvider) Open(context.Context, string, Grant) (Session, error) {
	p.session = &streamingSession{body: "file bytes"}
	return p.session, nil
}

type streamingSession struct {
	body     string
	reads    int
	closed   bool
	streamed int
}

func (s *streamingSession) Invoke(_ context.Context, action Action) (Result, error) {
	return Result{Output: json.RawMessage(`{"ok":true}`)}, nil
}

func (s *streamingSession) Stream(_ context.Context, action Action) (Content, error) {
	s.streamed++
	return &fakeContent{body: s.body, session: s}, nil
}

func (s *streamingSession) Close(context.Context) error {
	s.closed = true
	return nil
}

type fakeContent struct {
	body    string
	offset  int
	session *streamingSession
}

func (c *fakeContent) Read(p []byte) (int, error) {
	if c.offset >= len(c.body) {
		return 0, io.EOF
	}
	n := copy(p, c.body[c.offset:])
	c.offset += n
	return n, nil
}

func (c *fakeContent) Size() int64     { return int64(len(c.body)) }
func (c *fakeContent) Truncated() bool { return false }

func (c *fakeContent) Close() error {
	if c.session != nil {
		c.session.reads++
	}
	return nil
}

func TestConnectionStreamAppliesTheSameGrantAsInvoke(t *testing.T) {
	ctx := context.Background()
	provider := &streamingProvider{}
	registry, err := NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := registry.Open(ctx, OpenRequest{
		ProviderID: provider.ID(), DeviceID: "drive-1",
		Grant: Grant{Capabilities: []Capability{DriveRead}, ResourceIDs: []string{"photos"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)

	// A capability the grant does not include must not reach the provider.
	if _, err := connection.Stream(ctx, Action{Capability: DriveList, ResourceID: "photos"}); err == nil {
		t.Fatal("an ungranted stream reached the provider")
	}
	if provider.session.streamed != 0 {
		t.Fatal("an ungranted stream was opened by the provider")
	}
	// A resource the grant does not include must not reach the provider either.
	if _, err := connection.Stream(ctx, Action{Capability: DriveRead, ResourceID: "other"}); err == nil {
		t.Fatal("a stream outside the granted root reached the provider")
	}
	if _, err := connection.Stream(ctx, Action{Capability: DriveRead}); err == nil {
		t.Fatal("a drive stream without a resource ID reached the provider")
	}
	if _, err := connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: "photos", Input: json.RawMessage(`not-json`),
	}); err == nil {
		t.Fatal("a stream with invalid input JSON reached the provider")
	}
	if provider.session.streamed != 0 {
		t.Fatalf("provider opened %d streams for refused actions", provider.session.streamed)
	}

	content, err := connection.Stream(ctx, Action{Capability: DriveRead, ResourceID: "photos"})
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	if content.Size() != int64(len("file bytes")) {
		t.Fatalf("size = %d", content.Size())
	}
}

func TestConnectionStreamReportsASessionThatCannotStream(t *testing.T) {
	ctx := context.Background()
	// The plain fakeProvider's session is a Session, not a StreamingSession.
	registry, err := NewRegistry(&fakeProvider{devices: []Device{{
		ID: "drive-1", Kind: "drive",
		Capabilities: []CapabilityDescriptor{capability(DriveList), capability(DriveRead)},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := registry.Open(ctx, OpenRequest{
		ProviderID: "test", DeviceID: "drive-1",
		Grant: Grant{Capabilities: []Capability{DriveRead}, ResourceIDs: []string{"photos"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)
	if _, err := connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: "photos",
	}); !errors.Is(err, ErrNotStreamable) {
		t.Fatalf("stream from a non-streaming session = %v, want ErrNotStreamable", err)
	}
}

func TestConnectionStreamRefusesAClosedConnection(t *testing.T) {
	ctx := context.Background()
	provider := &streamingProvider{}
	registry, err := NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := registry.Open(ctx, OpenRequest{
		ProviderID: provider.ID(), DeviceID: "drive-1",
		Grant: Grant{Capabilities: []Capability{DriveRead}, ResourceIDs: []string{"photos"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Stream(ctx, Action{
		Capability: DriveRead, ResourceID: "photos",
	}); !errors.Is(err, ErrClosed) {
		t.Fatalf("stream after close = %v, want ErrClosed", err)
	}
}

func TestReadAllStopsAtTheHostLimit(t *testing.T) {
	if _, err := ReadAll(nil, 10); err == nil {
		t.Fatal("a nil stream was accepted")
	}
	if _, err := ReadAll(&fakeContent{body: "abc"}, 0); err == nil {
		t.Fatal("a zero host limit was accepted")
	}
	bytes, err := ReadAll(&fakeContent{body: "abcdefghij"}, 4)
	if err == nil {
		t.Fatal("a stream past the host limit was accepted")
	}
	if string(bytes) != "abcd" {
		t.Fatalf("bytes = %q, want the first 4 bytes", bytes)
	}
	if !strings.Contains(err.Error(), "host limit") {
		t.Fatalf("error = %v", err)
	}
	bytes, err = ReadAll(&fakeContent{body: "abcdefghij"}, 10)
	if err != nil || string(bytes) != "abcdefghij" {
		t.Fatalf("bytes = %q, %v", bytes, err)
	}
}
