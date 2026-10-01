package device_test

import (
	"board"
	"board/device"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestSessionChannelGrantsLimitsAndClose(t *testing.T) {
	var remote net.Conn
	p := &device.Provider{ProviderID: "test", ResourceIDs: []string{"approved"}, Device: board.Device{ID: "device", Capabilities: []board.CapabilityDescriptor{device.Descriptor(board.RemoteConnect, device.EmptySchema, true)}}, Bind: func(context.Context, board.Grant) (device.Binding, error) {
		return device.Binding{Operations: map[board.Capability]device.Operation{board.RemoteConnect: {Channel: func(context.Context, board.Action, board.ChannelLimits) (io.ReadWriteCloser, error) {
			a, b := net.Pipe()
			remote = b
			return a, nil
		}}}}, nil
	}}
	registry, err := board.NewRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	req := board.OpenRequest{ProviderID: "test", DeviceID: "device", Grant: board.Grant{Capabilities: []board.Capability{board.RemoteConnect}}}
	if _, err = registry.Open(context.Background(), req); err == nil {
		t.Fatal("resource-less grant accepted")
	}
	req.Grant.ResourceIDs = []string{"approved"}
	c, err := registry.Open(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	a := board.Action{Capability: board.RemoteConnect, ResourceID: "other"}
	limits := board.ChannelLimits{ReadBytes: 3, WriteBytes: 2, Duration: time.Minute}
	if _, err = c.OpenChannel(context.Background(), a, limits); !errors.Is(err, board.ErrNotGranted) {
		t.Fatalf("ungranted resource: %v", err)
	}
	a.ResourceID = "approved"
	ch, err := c.OpenChannel(context.Background(), a, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	go remote.Write([]byte("abc"))
	buf := make([]byte, 5)
	if n, err := ch.Read(buf); err != nil || n != 3 {
		t.Fatalf("read %d: %v", n, err)
	}
	if _, err = ch.Read(buf); !errors.Is(err, board.ErrLimit) {
		t.Fatal(err)
	}
	if _, err = ch.Write([]byte("abc")); !errors.Is(err, board.ErrLimit) {
		t.Fatal(err)
	}
	ch.Close()
	ch, err = c.OpenChannel(context.Background(), a, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	done := make(chan error, 1)
	go func() { _, err := ch.Read(buf); done <- err }()
	if err = c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close did not unblock read")
	}
	if _, err = c.Invoke(context.Background(), a); !errors.Is(err, board.ErrClosed) {
		t.Fatal(err)
	}
	events, err := c.Events(0)
	if err != nil || len(events.Events) < 4 {
		t.Fatalf("events: %+v %v", events, err)
	}
}
func TestDecodeStrict(t *testing.T) {
	for _, raw := range []string{`{"unexpected":1}`, `null`, `{} {}`} {
		if err := device.Empty([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if err := device.Empty([]byte(" \n{}")); err != nil {
		t.Fatal(err)
	}
}
