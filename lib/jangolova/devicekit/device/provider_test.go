package device_test

import (
	"context"
	"errors"
	"io"
	"jangolova/devicekit"
	"jangolova/devicekit/device"
	"net"
	"testing"
	"time"
)

func TestSessionChannelGrantsLimitsAndClose(t *testing.T) {
	var remote net.Conn
	p := &device.Provider{ProviderID: "test", ResourceIDs: []string{"approved"}, Device: devicekit.Device{ID: "device", Capabilities: []devicekit.CapabilityDescriptor{device.Descriptor(devicekit.RemoteConnect, device.EmptySchema, true)}}, Bind: func(context.Context, devicekit.Grant) (device.Binding, error) {
		return device.Binding{Operations: map[devicekit.Capability]device.Operation{devicekit.RemoteConnect: {Channel: func(context.Context, devicekit.Action, devicekit.ChannelLimits) (io.ReadWriteCloser, error) {
			a, b := net.Pipe()
			remote = b
			return a, nil
		}}}}, nil
	}}
	registry, err := devicekit.NewRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	req := devicekit.OpenRequest{ProviderID: "test", DeviceID: "device", Grant: devicekit.Grant{Capabilities: []devicekit.Capability{devicekit.RemoteConnect}}}
	if _, err = registry.Open(context.Background(), req); err == nil {
		t.Fatal("resource-less grant accepted")
	}
	req.Grant.ResourceIDs = []string{"approved"}
	c, err := registry.Open(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	a := devicekit.Action{Capability: devicekit.RemoteConnect, ResourceID: "other"}
	limits := devicekit.ChannelLimits{ReadBytes: 3, WriteBytes: 2, Duration: time.Minute}
	if _, err = c.OpenChannel(context.Background(), a, limits); !errors.Is(err, devicekit.ErrNotGranted) {
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
	if _, err = ch.Read(buf); !errors.Is(err, devicekit.ErrLimit) {
		t.Fatal(err)
	}
	if _, err = ch.Write([]byte("abc")); !errors.Is(err, devicekit.ErrLimit) {
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
	if _, err = c.Invoke(context.Background(), a); !errors.Is(err, devicekit.ErrClosed) {
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
