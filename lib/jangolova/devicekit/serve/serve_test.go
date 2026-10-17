package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"jangolova/devicekit"
)

type fakeProvider struct {
	id      string
	devices []devicekit.Device
	opened  int
}

func (p *fakeProvider) ID() string { return p.id }
func (p *fakeProvider) List(context.Context) ([]devicekit.Device, error) {
	return p.devices, nil
}
func (p *fakeProvider) Open(_ context.Context, id string, _ devicekit.Grant) (devicekit.Session, error) {
	p.opened++
	return &fakeSession{}, nil
}

type fakeSession struct{ closed bool }

func (s *fakeSession) Invoke(_ context.Context, action devicekit.Action) (devicekit.Result, error) {
	return devicekit.Result{Output: json.RawMessage(`{"capability":"` + string(action.Capability) + `"}`)}, nil
}
func (s *fakeSession) Stream(context.Context, devicekit.Action) (devicekit.Content, error) {
	return &fakeContent{Reader: strings.NewReader("0123456789")}, nil
}
func (s *fakeSession) Close(context.Context) error { s.closed = true; return nil }

type fakeContent struct {
	io.Reader
	closed bool
}

func (c *fakeContent) Size() int64     { return 10 }
func (c *fakeContent) Truncated() bool { return false }
func (c *fakeContent) Close() error    { c.closed = true; return nil }

func device(id string) devicekit.Device {
	return devicekit.Device{ID: id, Kind: "test", Capabilities: []devicekit.CapabilityDescriptor{{Name: "test.read"}}}
}

func newHandler(settingsSeen *map[string]string) (*Handler, *fakeProvider, *fakeProvider) {
	a := &fakeProvider{id: "alpha", devices: []devicekit.Device{device("a1")}}
	b := &fakeProvider{id: "beta", devices: []devicekit.Device{device("b1"), device("b2")}}
	return NewHandler(func(_ context.Context, settings map[string]string) ([]devicekit.Provider, error) {
		if settingsSeen != nil {
			*settingsSeen = settings
		}
		return []devicekit.Provider{b, a}, nil
	}), a, b
}

func call(t *testing.T, h *Handler, method string, params any) json.RawMessage {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		raw = encoded
	}
	result, err := h.Call(context.Background(), "device.provider."+method, raw)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestListReportsEveryProviderWithItsOwnID(t *testing.T) {
	var settings map[string]string
	h, _, _ := newHandler(&settings)
	var devices []devicekit.Device
	if err := json.Unmarshal(call(t, h, "list", map[string]any{"settings": map[string]string{"pid": "7"}}), &devices); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, d := range devices {
		got = append(got, d.ProviderID+"/"+d.ID)
	}
	if strings.Join(got, " ") != "alpha/a1 beta/b1 beta/b2" {
		t.Fatalf("devices = %v", got)
	}
	if settings["pid"] != "7" {
		t.Fatalf("settings = %v", settings)
	}
	var filtered []devicekit.Device
	if err := json.Unmarshal(call(t, h, "list", map[string]any{"provider": "beta"}), &filtered); err != nil || len(filtered) != 2 {
		t.Fatalf("filtered = %v, %v", filtered, err)
	}
}

func TestOpenChoosesTheProviderThatOwnsTheDevice(t *testing.T) {
	h, a, b := newHandler(nil)
	call(t, h, "open", map[string]any{"deviceId": "b2", "grant": devicekit.Grant{Capabilities: []devicekit.Capability{"test.read"}}})
	if a.opened != 0 || b.opened != 1 {
		t.Fatalf("opened alpha=%d beta=%d", a.opened, b.opened)
	}
	if _, err := h.Call(context.Background(), "device.provider.open", json.RawMessage(`{"deviceId":"a1","grant":{"capabilities":["test.read"]}}`)); err == nil {
		t.Fatal("a second session opened in one process")
	}
	if got := call(t, h, "invoke", devicekit.Action{Capability: "test.read"}); !bytes.Contains(got, []byte(`test.read`)) {
		t.Fatalf("invoke = %s", got)
	}
}

func TestOpenRejectsAnUnknownDevice(t *testing.T) {
	h, _, _ := newHandler(nil)
	_, err := h.Call(context.Background(), "device.provider.open", json.RawMessage(`{"deviceId":"nope","grant":{"capabilities":["test.read"]}}`))
	if !errors.Is(err, devicekit.ErrUnknownDevice) {
		t.Fatalf("error = %v", err)
	}
}

func TestStreamDeliversBoundedChunksUntilEOF(t *testing.T) {
	h, _, _ := newHandler(nil)
	call(t, h, "open", map[string]any{"deviceId": "a1", "grant": devicekit.Grant{Capabilities: []devicekit.Capability{"test.read"}}})
	var opened struct {
		ID   string `json:"id"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(call(t, h, "stream.open", devicekit.Action{Capability: "test.read"}), &opened); err != nil || opened.Size != 10 {
		t.Fatalf("stream.open = %+v, %v", opened, err)
	}
	var data []byte
	for {
		var chunk struct {
			Data []byte `json:"data"`
			EOF  bool   `json:"eof"`
		}
		if err := json.Unmarshal(call(t, h, "stream.read", map[string]any{"id": opened.ID, "maxBytes": 4}), &chunk); err != nil {
			t.Fatal(err)
		}
		if len(chunk.Data) > 4 {
			t.Fatalf("chunk of %d bytes exceeds the requested bound", len(chunk.Data))
		}
		data = append(data, chunk.Data...)
		if chunk.EOF {
			break
		}
	}
	if string(data) != "0123456789" {
		t.Fatalf("data = %q", data)
	}
	call(t, h, "stream.close", map[string]any{"id": opened.ID})
	if _, err := h.Call(context.Background(), "device.provider.stream.read", json.RawMessage(`{"id":"`+opened.ID+`"}`)); err == nil {
		t.Fatal("a closed stream was readable")
	}
	call(t, h, "close", nil)
}

func TestUnsupportedMethodIsRejected(t *testing.T) {
	h, _, _ := newHandler(nil)
	if _, err := h.Call(context.Background(), "device.provider.reboot", nil); err == nil {
		t.Fatal("an unknown operation was served")
	}
}
