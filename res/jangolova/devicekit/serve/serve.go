// Package serve runs device providers as an ext package that serves the
// device.provider contract. It is the guest side of the plugin adapter: a
// package main supplies the providers and this package speaks the protocol.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"github.com/webong/ext/pkg/plugin-go/contract/transport/jsonline"
	"jangolova/devicekit"
)

const (
	contract        = "device.provider"
	contractVersion = "v1"

	// maxChunk matches the bound the host enforces on one stream.read.
	maxChunk    = 32768
	maxStreams  = 8
	maxEmptyRun = 16
)

// Factory builds the providers a process serves from the host's non-secret
// settings. It is called once per process, on the first list or open.
type Factory func(ctx context.Context, settings map[string]string) ([]devicekit.Provider, error)

// Run loads the descriptor beside the executable and serves until the host
// closes the session.
func Run(ctx context.Context, factory Factory) error {
	path := filepath.Join(filepath.Dir(os.Args[0]), store.ManifestName)
	descriptor, err := packagekit.LoadDescriptor(path)
	if err != nil {
		return err
	}
	if !(packagekit.Manifest{Descriptor: descriptor}).Supports(contract, contractVersion) {
		return errors.New("plugin manifest does not declare the device.provider contract")
	}
	return jsonline.ServeFunc(ctx, descriptor, NewHandler(factory).Call, jsonline.FuncOptions{})
}

// Handler serves one device session per process.
type Handler struct {
	factory Factory

	mu        sync.Mutex
	providers []devicekit.Provider
	session   devicekit.Session
	streams   map[string]devicekit.Content
	next      int

	// The event log has its own lock so a poll never waits behind a long call.
	eventMu     sync.Mutex
	eventItems  []devicekit.Event
	eventCursor uint64
	eventStop   context.CancelFunc
}

// maxEvents bounds the log; a poller that falls this far behind loses the
// oldest events, never memory.
const maxEvents = 256

func NewHandler(factory Factory) *Handler {
	return &Handler{factory: factory, streams: map[string]devicekit.Content{}}
}

type selector struct {
	Provider string            `json:"provider,omitempty"`
	Settings map[string]string `json:"settings,omitempty"`
}

// Call dispatches one device.provider operation.
func (h *Handler) Call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if method == contract+".events" {
		var params struct {
			After uint64 `json:"after"`
		}
		if err := decode(raw, &params, false); err != nil {
			return nil, err
		}
		return h.eventsAfter(params.After), nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch method {
	case contract + ".list":
		var params selector
		if err := decode(raw, &params, false); err != nil {
			return nil, err
		}
		return h.list(ctx, params)
	case contract + ".open":
		var params struct {
			selector
			DeviceID string          `json:"deviceId"`
			Grant    devicekit.Grant `json:"grant"`
		}
		if err := decode(raw, &params, true); err != nil {
			return nil, err
		}
		return struct{}{}, h.open(ctx, params.selector, params.DeviceID, params.Grant)
	case contract + ".invoke":
		var action devicekit.Action
		if err := decode(raw, &action, true); err != nil {
			return nil, err
		}
		if h.session == nil {
			return nil, errors.New("no device session is open")
		}
		return h.session.Invoke(ctx, action)
	case contract + ".stream.open":
		var action devicekit.Action
		if err := decode(raw, &action, true); err != nil {
			return nil, err
		}
		return h.streamOpen(ctx, action)
	case contract + ".stream.read":
		var params struct {
			ID       string `json:"id"`
			MaxBytes int    `json:"maxBytes"`
		}
		if err := decode(raw, &params, true); err != nil {
			return nil, err
		}
		return h.streamRead(params.ID, params.MaxBytes)
	case contract + ".stream.close":
		var params struct {
			ID string `json:"id"`
		}
		if err := decode(raw, &params, true); err != nil {
			return nil, err
		}
		return struct{}{}, h.streamClose(params.ID)
	case contract + ".close":
		return struct{}{}, h.close(ctx)
	}
	return nil, fmt.Errorf("unsupported method %q", method)
}

func decode(raw json.RawMessage, destination any, required bool) error {
	if len(raw) == 0 || string(raw) == "null" {
		if required {
			return errors.New("method parameters are required")
		}
		return nil
	}
	return json.Unmarshal(raw, destination)
}

func (h *Handler) build(ctx context.Context, settings map[string]string) error {
	if h.providers != nil {
		return nil
	}
	providers, err := h.factory(ctx, settings)
	if err != nil {
		return err
	}
	h.providers = providers
	return nil
}

func (h *Handler) list(ctx context.Context, params selector) ([]devicekit.Device, error) {
	if err := h.build(ctx, params.Settings); err != nil {
		return nil, err
	}
	devices := []devicekit.Device{}
	for _, provider := range h.providers {
		if params.Provider != "" && provider.ID() != params.Provider {
			continue
		}
		listed, err := provider.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list provider %q: %w", provider.ID(), err)
		}
		for _, device := range listed {
			device.ProviderID = provider.ID()
			devices = append(devices, device)
		}
	}
	sort.SliceStable(devices, func(i, j int) bool {
		if devices[i].ProviderID != devices[j].ProviderID {
			return devices[i].ProviderID < devices[j].ProviderID
		}
		return devices[i].ID < devices[j].ID
	})
	return devices, nil
}

func (h *Handler) open(ctx context.Context, params selector, deviceID string, grant devicekit.Grant) error {
	if h.session != nil {
		return errors.New("a device session is already open")
	}
	if err := h.build(ctx, params.Settings); err != nil {
		return err
	}
	var chosen devicekit.Provider
	for _, provider := range h.providers {
		if params.Provider != "" && provider.ID() != params.Provider {
			continue
		}
		listed, err := provider.List(ctx)
		if err != nil {
			return fmt.Errorf("list provider %q: %w", provider.ID(), err)
		}
		for _, device := range listed {
			if device.ID == deviceID {
				chosen = provider
			}
		}
		if chosen != nil {
			break
		}
	}
	if chosen == nil {
		return fmt.Errorf("%w: %q", devicekit.ErrUnknownDevice, deviceID)
	}
	session, err := chosen.Open(ctx, deviceID, grant)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("provider returned a nil session")
	}
	h.session = session
	if source, ok := session.(devicekit.EventSource); ok {
		h.watch(source.Events())
	}
	return nil
}

// watch copies a session's events into the log until the session closes.
func (h *Handler) watch(events <-chan devicekit.Event) {
	ctx, stop := context.WithCancel(context.Background())
	h.eventStop = stop
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, open := <-events:
				if !open {
					return
				}
				h.record(event)
			}
		}
	}()
}

func (h *Handler) record(event devicekit.Event) {
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	h.eventCursor++
	event.Cursor = h.eventCursor
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	if len(h.eventItems) == maxEvents {
		copy(h.eventItems, h.eventItems[1:])
		h.eventItems = h.eventItems[:maxEvents-1]
	}
	h.eventItems = append(h.eventItems, event)
}

// eventsAfter returns the logged events newer than the cursor.
func (h *Handler) eventsAfter(after uint64) devicekit.EventBatch {
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	batch := devicekit.EventBatch{Cursor: h.eventCursor, Events: []devicekit.Event{}}
	for _, event := range h.eventItems {
		if event.Cursor > after {
			batch.Events = append(batch.Events, event)
		}
	}
	return batch
}

func (h *Handler) streamOpen(ctx context.Context, action devicekit.Action) (any, error) {
	if h.session == nil {
		return nil, errors.New("no device session is open")
	}
	streamer, ok := h.session.(devicekit.StreamingSession)
	if !ok {
		return nil, fmt.Errorf("%w: %q", devicekit.ErrNotStreamable, action.Capability)
	}
	if len(h.streams) >= maxStreams {
		return nil, errors.New("too many open streams")
	}
	content, err := streamer.Stream(ctx, action)
	if err != nil {
		return nil, err
	}
	h.next++
	id := fmt.Sprintf("stream-%d", h.next)
	h.streams[id] = content
	return struct {
		ID        string `json:"id"`
		Size      int64  `json:"size"`
		Truncated bool   `json:"truncated"`
	}{id, content.Size(), content.Truncated()}, nil
}

func (h *Handler) streamRead(id string, maxBytes int) (any, error) {
	content, ok := h.streams[id]
	if !ok {
		return nil, fmt.Errorf("unknown stream %q", id)
	}
	if maxBytes <= 0 || maxBytes > maxChunk {
		maxBytes = maxChunk
	}
	buffer := make([]byte, maxBytes)
	for empty := 0; ; empty++ {
		n, err := content.Read(buffer)
		eof := errors.Is(err, io.EOF)
		if err != nil && !eof {
			return nil, err
		}
		if n > 0 || eof {
			return struct {
				Data []byte `json:"data"`
				EOF  bool   `json:"eof"`
			}{buffer[:n], eof}, nil
		}
		if empty >= maxEmptyRun {
			return nil, errors.New("stream returned no data")
		}
	}
}

func (h *Handler) streamClose(id string) error {
	content, ok := h.streams[id]
	if !ok {
		return nil
	}
	delete(h.streams, id)
	return content.Close()
}

func (h *Handler) close(ctx context.Context) error {
	var errs []error
	for id := range h.streams {
		errs = append(errs, h.streamClose(id))
	}
	if h.eventStop != nil {
		h.eventStop()
		h.eventStop = nil
	}
	if h.session != nil {
		errs = append(errs, h.session.Close(ctx))
		h.session = nil
	}
	return errors.Join(errs...)
}
