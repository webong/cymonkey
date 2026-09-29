//go:build windows

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type windowsRuntime struct {
	config   helperConfig
	mu       sync.Mutex
	sequence uint64
	events   []semanticEvent
}

type capability struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Domain        string          `json:"domain"`
	Runtime       string          `json:"runtime"`
	Driver        string          `json:"driver"`
	Support       string          `json:"support"`
	Lifetime      string          `json:"lifetime"`
	Persistence   string          `json:"persistence"`
	Effect        string          `json:"effect"`
	ResourceKinds []string        `json:"resourceKinds,omitempty"`
	InputSchema   json.RawMessage `json:"inputSchema"`
}

type semanticEvent struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	OccurredAt string         `json:"occurredAt"`
	Domain     string         `json:"domain"`
	Runtime    string         `json:"runtime"`
	Driver     string         `json:"driver"`
	SurfaceID  string         `json:"surfaceId,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

type windowsSurface struct {
	ID         string         `json:"id"`
	Domain     string         `json:"domain"`
	Runtime    string         `json:"runtime"`
	Kind       string         `json:"kind"`
	Label      string         `json:"label,omitempty"`
	Properties map[string]any `json:"properties"`
}

func newWindowsRuntime(config helperConfig) *windowsRuntime { return &windowsRuntime{config: config} }

func (r *windowsRuntime) handle(request controlRequest) controlResponse {
	var result any
	var err *controlError
	switch request.Method {
	case "hello":
		result = r.hello()
	case "capabilities":
		result = r.capabilities()
	case "describe":
		result, err = r.describe()
	case "act":
		result, err = r.act(request.Params)
	case "events":
		result, err = r.readEvents(request.Params)
	default:
		err = &controlError{Code: "invalid_request", Message: "unsupported Cymonkey method"}
	}
	if err != nil {
		return controlResponse{ID: request.ID, Error: err}
	}
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return controlResponse{ID: request.ID, Error: &controlError{Code: "internal", Message: "native helper operation failed"}}
	}
	return controlResponse{ID: request.ID, Result: encoded}
}

func (r *windowsRuntime) hello() map[string]any {
	runtimes := []string{"windows-app"}
	drivers := []string{"windows-win32"}
	if r.config.Viewer != nil && r.config.Viewer.Enabled {
		runtimes = append(runtimes, "windows-viewer")
		drivers = append(drivers, "windows-viewer")
	}
	return map[string]any{
		"protocolVersion": protocolVersion,
		"implementation":  map[string]any{"name": "jangolova-cymonkey-windows-helper", "version": "0.1.0"},
		"domains":         []string{"viewer"}, "runtimes": runtimes, "drivers": drivers,
		"features": []string{"events.cursor", "owner.allowlist", "surface.scoped"},
	}
}

func (r *windowsRuntime) capabilities() []capability {
	objectSchema := func(required ...string) json.RawMessage {
		value, _ := json.Marshal(map[string]any{"type": "object", "required": required, "additionalProperties": true})
		return value
	}
	app := func(name, description, effect string, required ...string) capability {
		return capability{Name: name, Description: description, Domain: "viewer", Runtime: "windows-app", Driver: "windows-win32", Support: "mapped", Lifetime: "attachment", Persistence: "session", Effect: effect, ResourceKinds: []string{"window"}, InputSchema: objectSchema(required...)}
	}
	result := []capability{
		app("window.list", "List allowlisted top-level application windows.", "read"),
		app("window.describe", "Describe one attachment-scoped allowlisted window.", "read", "surfaceId"),
		app("window.activate", "Activate one attachment-scoped allowlisted window.", "write", "surfaceId"),
	}
	if policy := r.config.Viewer; policy != nil && policy.Enabled {
		viewer := func(name, description, effect string, required ...string) capability {
			return capability{Name: name, Description: description, Domain: "viewer", Runtime: "windows-viewer", Driver: "windows-viewer", Support: "native", Lifetime: "attachment", Persistence: "ephemeral", Effect: effect, ResourceKinds: []string{"window", "viewport"}, InputSchema: objectSchema(required...)}
		}
		if policy.AllowCapture {
			result = append(result, viewer("display.describe", "Describe an allowlisted viewer surface.", "read", "surfaceId"), viewer("display.capture", "Capture an allowlisted viewer surface.", "read", "surfaceId"))
		}
		if policy.AllowInput {
			result = append(result,
				viewer("pointer.move", "Move the pointer inside an allowlisted viewer surface.", "write", "surfaceId", "x", "y"),
				viewer("pointer.click", "Click inside an allowlisted viewer surface.", "write", "surfaceId", "x", "y"),
				viewer("pointer.drag", "Drag inside an allowlisted viewer surface.", "write", "surfaceId", "startX", "startY", "endX", "endY"),
				viewer("pointer.scroll", "Scroll inside an allowlisted viewer surface.", "write", "surfaceId", "x", "y", "deltaY"),
				viewer("keyboard.type", "Type text into an activated allowlisted viewer surface.", "write", "surfaceId", "text"),
				viewer("keyboard.press", "Press an allowlisted key in an activated viewer surface.", "write", "surfaceId", "key"),
			)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (r *windowsRuntime) describe() (any, *controlError) {
	windows, err := enumerateWindows(r.config)
	if err != nil {
		return nil, nativeError(err)
	}
	surfaces := make([]windowsSurface, 0, len(windows)*2)
	for _, window := range windows {
		surfaces = append(surfaces, window.appSurface())
		if r.config.Viewer != nil && r.config.Viewer.Enabled {
			surfaces = append(surfaces, window.viewerSurface())
		}
	}
	revisionParts := make([]string, 0, len(surfaces))
	for _, surface := range surfaces {
		revisionParts = append(revisionParts, surface.ID)
	}
	return map[string]any{"revision": strings.Join(revisionParts, "|"), "surfaces": surfaces, "augmentations": []any{}}, nil
}

func (r *windowsRuntime) act(raw json.RawMessage) (any, *controlError) {
	var request struct {
		Name  string                     `json:"name"`
		Input map[string]json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &request); err != nil || request.Input == nil || strings.TrimSpace(request.Name) == "" {
		return nil, invalid("invalid Cymonkey action")
	}
	getSurface := func(prefix string) (nativeWindow, *controlError) {
		surfaceID, err := requiredString(request.Input, "surfaceId")
		if err != nil {
			return nativeWindow{}, invalid(err.Error())
		}
		window, err := resolveWindow(r.config, prefix, surfaceID)
		if err != nil {
			return nativeWindow{}, stale(err.Error())
		}
		return window, nil
	}
	switch request.Name {
	case "window.list":
		windows, err := enumerateWindows(r.config)
		if err != nil {
			return nil, nativeError(err)
		}
		values := make([]windowsSurface, 0, len(windows))
		for _, window := range windows {
			values = append(values, window.appSurface())
		}
		return values, nil
	case "window.describe":
		window, problem := getSurface("windows-app:")
		if problem != nil {
			return nil, problem
		}
		return window.appSurface(), nil
	case "window.activate":
		window, problem := getSurface("windows-app:")
		if problem != nil {
			return nil, problem
		}
		if err := window.activate(); err != nil {
			return nil, nativeError(err)
		}
		r.publish("window.activated", "windows-app", "windows-win32", window.appSurface().ID, nil)
		return map[string]bool{"ok": true}, nil
	case "display.describe":
		if !r.canCapture() {
			return nil, denied("viewer capture is not enabled")
		}
		window, problem := getSurface("windows-viewer:")
		if problem != nil {
			return nil, problem
		}
		return window.viewerDescription(), nil
	case "display.capture":
		if !r.canCapture() {
			return nil, denied("viewer capture is not enabled")
		}
		window, problem := getSurface("windows-viewer:")
		if problem != nil {
			return nil, problem
		}
		capture, err := window.capture()
		if err != nil {
			return nil, nativeError(err)
		}
		r.publish("display.captured", "windows-viewer", "windows-viewer", window.viewerSurface().ID, nil)
		return capture, nil
	case "pointer.move", "pointer.click", "pointer.drag", "pointer.scroll", "keyboard.type", "keyboard.press":
		if !r.canInput() {
			return nil, denied("viewer input is not enabled")
		}
		window, problem := getSurface("windows-viewer:")
		if problem != nil {
			return nil, problem
		}
		return r.viewerAct(request.Name, request.Input, window)
	default:
		return nil, denied("Cymonkey capability was not advertised")
	}
}

func (r *windowsRuntime) viewerAct(name string, input map[string]json.RawMessage, window nativeWindow) (any, *controlError) {
	if err := window.activate(); err != nil {
		return nil, nativeError(err)
	}
	readCoordinate := func(name string) (int, error) {
		var result int
		raw, ok := input[name]
		if !ok || json.Unmarshal(raw, &result) != nil {
			return 0, fmt.Errorf("%s must be an integer", name)
		}
		return result, nil
	}
	within := func(x, y int) error { return window.validateLocalPoint(x, y) }
	surfaceID := window.viewerSurface().ID
	switch name {
	case "pointer.move", "pointer.click", "pointer.scroll":
		x, xErr := readCoordinate("x")
		y, yErr := readCoordinate("y")
		if xErr != nil {
			return nil, invalid(xErr.Error())
		}
		if yErr != nil {
			return nil, invalid(yErr.Error())
		}
		if err := within(x, y); err != nil {
			return nil, denied(err.Error())
		}
		if err := window.movePointer(x, y); err != nil {
			return nil, nativeError(err)
		}
		if name == "pointer.click" {
			button := optionalString(input, "button")
			if err := click(button); err != nil {
				return nil, invalid(err.Error())
			}
		}
		if name == "pointer.scroll" {
			delta, err := readCoordinate("deltaY")
			if err != nil {
				return nil, invalid(err.Error())
			}
			if err := scroll(delta); err != nil {
				return nil, nativeError(err)
			}
		}
	case "pointer.drag":
		sx, a := readCoordinate("startX")
		sy, b := readCoordinate("startY")
		ex, c := readCoordinate("endX")
		ey, d := readCoordinate("endY")
		if a != nil || b != nil || c != nil || d != nil {
			return nil, invalid("pointer.drag requires integer startX, startY, endX, and endY")
		}
		if err := within(sx, sy); err != nil {
			return nil, denied(err.Error())
		}
		if err := within(ex, ey); err != nil {
			return nil, denied(err.Error())
		}
		if err := window.drag(sx, sy, ex, ey); err != nil {
			return nil, nativeError(err)
		}
	case "keyboard.type":
		text, err := requiredString(input, "text")
		if err != nil {
			return nil, invalid(err.Error())
		}
		if max := r.config.Viewer.MaxTextLength; max > 0 && len([]rune(text)) > max {
			return nil, denied("keyboard text exceeds owner policy")
		}
		if err := typeText(text); err != nil {
			return nil, nativeError(err)
		}
		details := map[string]any{"text": text}
		if r.config.Viewer.RedactTypedInput || optionalBool(input, "sensitive") {
			details["text"] = "***REDACTED***"
		}
		r.publish("keyboard.typed", "windows-viewer", "windows-viewer", surfaceID, details)
		return map[string]bool{"ok": true}, nil
	case "keyboard.press":
		key, err := requiredString(input, "key")
		if err != nil {
			return nil, invalid(err.Error())
		}
		key = canonicalKey(key)
		if r.blockedKey(key) {
			return nil, denied("key is blocked by viewer policy")
		}
		if err := pressKey(key); err != nil {
			return nil, invalid(err.Error())
		}
	}
	r.publish(strings.ReplaceAll(name, ".", ".")+".invoked", "windows-viewer", "windows-viewer", surfaceID, nil)
	return map[string]bool{"ok": true}, nil
}

func (r *windowsRuntime) canCapture() bool {
	return r.config.Viewer != nil && r.config.Viewer.Enabled && r.config.Viewer.AllowCapture
}
func (r *windowsRuntime) canInput() bool {
	return r.config.Viewer != nil && r.config.Viewer.Enabled && r.config.Viewer.AllowInput
}
func (r *windowsRuntime) blockedKey(key string) bool {
	for _, blocked := range r.config.Viewer.BlockedKeys {
		if key == blocked {
			return true
		}
	}
	return false
}

func (r *windowsRuntime) readEvents(raw json.RawMessage) (any, *controlError) {
	var request struct {
		After string `json:"after"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal(raw, &request)
	after, _ := strconv.ParseUint(request.After, 10, 64)
	limit := request.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make([]semanticEvent, 0, limit)
	for _, event := range r.events {
		id, _ := strconv.ParseUint(event.ID, 10, 64)
		if id > after {
			values = append(values, event)
			if len(values) == limit {
				break
			}
		}
	}
	cursor := request.After
	if len(values) > 0 {
		cursor = values[len(values)-1].ID
	}
	if cursor == "" {
		cursor = "0"
	}
	return map[string]any{"events": values, "cursor": cursor}, nil
}

func (r *windowsRuntime) publish(kind, runtime, driver, surfaceID string, data map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sequence++
	r.events = append(r.events, semanticEvent{ID: strconv.FormatUint(r.sequence, 10), Type: kind, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), Domain: "viewer", Runtime: runtime, Driver: driver, SurfaceID: surfaceID, Data: data})
	if len(r.events) > 1000 {
		r.events = r.events[len(r.events)-1000:]
	}
}

func requiredString(input map[string]json.RawMessage, name string) (string, error) {
	var value string
	raw, ok := input[name]
	if !ok || json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}
func optionalString(input map[string]json.RawMessage, name string) string {
	var value string
	_ = json.Unmarshal(input[name], &value)
	return value
}
func optionalBool(input map[string]json.RawMessage, name string) bool {
	var value bool
	_ = json.Unmarshal(input[name], &value)
	return value
}
func invalid(message string) *controlError {
	return &controlError{Code: "invalid_request", Message: message}
}
func denied(message string) *controlError { return &controlError{Code: "denied", Message: message} }
func stale(message string) *controlError {
	return &controlError{Code: "stale_reference", Message: message}
}
func nativeError(err error) *controlError {
	if errors.Is(err, errUnavailable) {
		return &controlError{Code: "unavailable", Message: "native desktop operation is unavailable"}
	}
	return &controlError{Code: "native_failure", Message: "native desktop operation failed"}
}

type frameCapture struct {
	Format     string `json:"format"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Base64Data string `json:"base64Data"`
}

func encodePNG(width, height int, rgba []byte) (frameCapture, error) {
	if width <= 0 || height <= 0 || len(rgba) != width*height*4 {
		return frameCapture{}, errors.New("invalid capture dimensions")
	}
	var output bytes.Buffer
	if err := encodeRGBAAsPNG(&output, width, height, rgba); err != nil {
		return frameCapture{}, err
	}
	return frameCapture{Format: "image/png", Width: width, Height: height, Base64Data: base64.StdEncoding.EncodeToString(output.Bytes())}, nil
}
