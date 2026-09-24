// Package webdriverclassic implements interaction with an existing W3C
// WebDriver session. The target provider owns the driver, browser, and session;
// disconnecting this adapter deliberately never sends DELETE /session.
package webdriverclassic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"cymonkey/src/internal/bridge"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	"cymonkey/src/targetconn"
)

const elementKey = "element-6066-11e4-a52e-4f735466cecf"

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,256}$`)

type Adapter struct {
	implementation string
	webkit         bool
}

// Generic attaches to any caller-owned W3C WebDriver Classic session.
func Generic() Adapter { return Adapter{implementation: "webdriver-classic"} }

// WebKit attaches to a caller-owned WebKitGTK, WPE WebKit, or Safari
// WebDriver session without owning the driver or browser process.
func WebKit() Adapter { return Adapter{implementation: "webkit-webdriver", webkit: true} }

type options struct {
	RequestTimeout string `json:"requestTimeout,omitempty"`
}

type instance struct {
	implementation string
	baseURL        string
	sessionID      string
	client         *http.Client
	callMu         sync.Mutex
	stateMu        sync.Mutex
	disconnected   bool
	events         []bridge.Event
	nextEvent      uint64
	lifecycle      chan orchestrator.EngineEvent
	closeOnce      sync.Once
}

type webdriverEnvelope struct {
	Value json.RawMessage `json:"value"`
}

var _ orchestrator.EngineAdapter = Adapter{}
var _ orchestrator.EngineInspector = Adapter{}
var _ orchestrator.EngineInstance = (*instance)(nil)
var _ orchestrator.EngineHealthProvider = (*instance)(nil)
var _ orchestrator.EngineCapabilityProvider = (*instance)(nil)
var _ orchestrator.EngineEventSource = (*instance)(nil)
var _ bridge.Caller = (*instance)(nil)

func (a Adapter) InspectEngine(context.Context) orchestrator.EngineInspection {
	capabilities := []string{"act", "capabilities", "describe", "events", "target.webdriver"}
	if a.webkit {
		capabilities = append(capabilities, "target.webkit.webdriver")
	}
	return orchestrator.EngineInspection{
		Available:    true,
		Capabilities: append(capabilities, capabilityNames()...),
	}
}

func (a Adapter) Connect(
	ctx context.Context,
	spec manifest.EngineSpec,
	target orchestrator.EngineTarget,
) (orchestrator.EngineInstance, error) {
	if target.Kind != "browser" {
		return nil, errors.New("WebDriver Classic requires target.kind browser")
	}
	endpoint, ok := target.Endpoint("webdriver")
	if !ok {
		return nil, errors.New("WebDriver Classic requires a caller-owned webdriver endpoint")
	}
	baseURL, err := validateEndpoint(endpoint.URL)
	if err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(target.Handles["webdriver.sessionId"])
	if !sessionIDPattern.MatchString(sessionID) {
		return nil, errors.New("target handle webdriver.sessionId is required")
	}
	config, err := decodeOptions(spec.Options)
	if err != nil {
		return nil, err
	}
	timeout := 30 * time.Second
	if config.RequestTimeout != "" {
		timeout, err = time.ParseDuration(config.RequestTimeout)
		if err != nil || timeout <= 0 {
			return nil, fmt.Errorf("invalid WebDriver requestTimeout %q", config.RequestTimeout)
		}
	}
	client, err := targetconn.HTTPClient(endpoint, timeout)
	if err != nil {
		return nil, err
	}
	running := &instance{
		implementation: a.name(),
		baseURL:        baseURL,
		sessionID:      sessionID,
		client:         client,
		lifecycle:      make(chan orchestrator.EngineEvent, 1),
	}
	if _, err := running.command(ctx, http.MethodGet, "/url", nil); err != nil {
		return nil, fmt.Errorf("attach to caller-owned WebDriver session: %w", err)
	}
	running.appendEvent("browser.connected", map[string]any{"adapter": a.name(), "protocol": "webdriver"})
	if source := strings.TrimSpace(spec.Source); source != "" {
		params, _ := json.Marshal(map[string]any{
			"name":  "window.navigate",
			"input": map[string]string{"url": source},
		})
		if _, err := running.Call(ctx, bridge.MethodAct, params); err != nil {
			_ = running.Disconnect(context.Background())
			return nil, fmt.Errorf("navigate WebDriver target: %w", err)
		}
	}
	return running, nil
}

func (i *instance) Disconnect(context.Context) error {
	i.stateMu.Lock()
	if i.disconnected {
		i.stateMu.Unlock()
		return nil
	}
	i.disconnected = true
	i.stateMu.Unlock()
	if transport, ok := i.client.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	} else {
		i.client.CloseIdleConnections()
	}
	i.closeOnce.Do(func() {
		i.lifecycle <- orchestrator.EngineEvent{
			Type: "interaction.disconnected", Status: "disconnected", OccurredAt: time.Now().UTC(),
		}
		close(i.lifecycle)
	})
	return nil
}

func (i *instance) EngineHealth(ctx context.Context) orchestrator.EngineHealth {
	health := orchestrator.EngineHealth{ObservedAt: time.Now().UTC()}
	if _, err := i.command(ctx, http.MethodGet, "/url", nil); err != nil {
		health.Status = orchestrator.EngineHealthUnhealthy
		health.Message = err.Error()
		return health
	}
	health.Status = orchestrator.EngineHealthHealthy
	return health
}

func (i *instance) Authorize(ctx context.Context, request orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	action := strings.TrimSpace(request.Action)
	if action == "" {
		return orchestrator.AuthorizeDecision{Authorized: false}, errors.New("WebDriver interaction action name is required")
	}
	for _, capability := range capabilityNames() {
		if capability == action {
			return orchestrator.AuthorizeDecision{Authorized: true}, nil
		}
	}
	return orchestrator.AuthorizeDecision{Authorized: false}, fmt.Errorf("WebDriver action %q was not advertised", action)
}

func (i *instance) EngineCapabilities() []string                  { return capabilityNames() }
func (i *instance) EngineEvents() <-chan orchestrator.EngineEvent { return i.lifecycle }

func (i *instance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	i.callMu.Lock()
	defer i.callMu.Unlock()
	switch method {
	case bridge.MethodHello:
		return json.Marshal(bridge.Hello{
			ProtocolVersion: bridge.ProtocolVersion,
			Implementation:  bridge.Implementation{Name: i.implementation},
			Features:        []string{"browser", "caller-owned-target", "events.local"},
		})
	case bridge.MethodCapabilities:
		return json.Marshal(capabilities())
	case bridge.MethodDescribe:
		return i.describe(ctx)
	case bridge.MethodAct:
		return i.act(ctx, params)
	case bridge.MethodEvents:
		return i.readEvents(params)
	default:
		return nil, fmt.Errorf("unsupported interaction method %q", method)
	}
}

func (i *instance) describe(ctx context.Context) (json.RawMessage, error) {
	currentURL, err := i.command(ctx, http.MethodGet, "/url", nil)
	if err != nil {
		return nil, err
	}
	title, err := i.command(ctx, http.MethodGet, "/title", nil)
	if err != nil {
		return nil, err
	}
	window, err := i.command(ctx, http.MethodGet, "/window", nil)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"adapter": i.implementation,
		"page":    map[string]json.RawMessage{"url": currentURL, "title": title, "window": window},
	})
}

func (a Adapter) name() string {
	if a.implementation != "" {
		return a.implementation
	}
	return "webdriver-classic"
}

func (i *instance) act(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var request struct {
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fmt.Errorf("decode WebDriver action: %w", err)
	}
	var result json.RawMessage
	var err error
	switch request.Name {
	case "window.navigate":
		value, valueErr := requiredString(request.Input, "url")
		if valueErr != nil {
			return nil, valueErr
		}
		result, err = i.command(ctx, http.MethodPost, "/url", map[string]string{"url": value})
	case "window.click":
		selector, valueErr := requiredString(request.Input, "selector")
		if valueErr != nil {
			return nil, valueErr
		}
		var id string
		id, err = i.findElement(ctx, selector)
		if err == nil {
			result, err = i.command(ctx, http.MethodPost, "/element/"+url.PathEscape(id)+"/click", map[string]any{})
		}
	case "window.fill":
		selector, valueErr := requiredString(request.Input, "selector")
		if valueErr != nil {
			return nil, valueErr
		}
		value, valueErr := requiredString(request.Input, "value")
		if valueErr != nil {
			return nil, valueErr
		}
		var id string
		id, err = i.findElement(ctx, selector)
		if err == nil {
			_, err = i.command(ctx, http.MethodPost, "/element/"+url.PathEscape(id)+"/clear", map[string]any{})
		}
		if err == nil {
			result, err = i.command(ctx, http.MethodPost, "/element/"+url.PathEscape(id)+"/value", map[string]any{"text": value, "value": strings.Split(value, "")})
		}
	case "window.press":
		key, valueErr := requiredString(request.Input, "key")
		if valueErr != nil {
			return nil, valueErr
		}
		result, err = i.command(ctx, http.MethodPost, "/actions", keyActions(webDriverKey(key)))
	case "window.evaluate":
		expression, valueErr := requiredString(request.Input, "expression")
		if valueErr != nil {
			return nil, valueErr
		}
		result, err = i.command(ctx, http.MethodPost, "/execute/sync", map[string]any{
			"script": "return eval(arguments[0]);", "args": []any{expression},
		})
	case "window.screenshot":
		result, err = i.command(ctx, http.MethodGet, "/screenshot", nil)
	default:
		return nil, fmt.Errorf("unsupported browser action %q", request.Name)
	}
	if err != nil {
		return nil, err
	}
	i.appendEvent("browser.action", map[string]any{"name": request.Name})
	if len(result) == 0 || string(result) == "null" {
		return json.RawMessage(`{}`), nil
	}
	if request.Name == "window.screenshot" {
		return json.Marshal(map[string]json.RawMessage{"pngBase64": result})
	}
	return result, nil
}

func (i *instance) findElement(ctx context.Context, selector string) (string, error) {
	value, err := i.command(ctx, http.MethodPost, "/element", map[string]string{"using": "css selector", "value": selector})
	if err != nil {
		return "", err
	}
	var element map[string]string
	if err := json.Unmarshal(value, &element); err != nil || element[elementKey] == "" {
		return "", errors.New("WebDriver returned no element identifier")
	}
	return element[elementKey], nil
}

func (i *instance) command(ctx context.Context, method, path string, payload any) (json.RawMessage, error) {
	i.stateMu.Lock()
	disconnected := i.disconnected
	i.stateMu.Unlock()
	if disconnected {
		return nil, errors.New("WebDriver interaction is disconnected")
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	requestURL := i.baseURL + "/session/" + url.PathEscape(i.sessionID) + path
	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := i.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return nil, err
	}
	var envelope webdriverEnvelope
	if err := json.Unmarshal(contents, &envelope); err != nil {
		return nil, fmt.Errorf("decode WebDriver response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(envelope.Value, &failure)
		if failure.Message == "" {
			failure.Message = response.Status
		}
		if failure.Error != "" {
			failure.Message = failure.Error + ": " + failure.Message
		}
		return nil, errors.New(failure.Message)
	}
	if len(envelope.Value) == 0 {
		return json.RawMessage(`null`), nil
	}
	return envelope.Value, nil
}

func (i *instance) appendEvent(eventType string, data any) {
	i.stateMu.Lock()
	defer i.stateMu.Unlock()
	i.nextEvent++
	raw, _ := json.Marshal(data)
	i.events = append(i.events, bridge.Event{ID: fmt.Sprint(i.nextEvent), Type: eventType, OccurredAt: time.Now().UTC(), Data: raw})
	if len(i.events) > 256 {
		i.events = append([]bridge.Event(nil), i.events[len(i.events)-256:]...)
	}
}

func (i *instance) readEvents(raw json.RawMessage) (json.RawMessage, error) {
	var query bridge.EventQuery
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &query); err != nil {
			return nil, err
		}
	}
	limit := query.Limit
	if limit <= 0 || limit > 256 {
		limit = 100
	}
	types := make(map[string]struct{}, len(query.Types))
	for _, value := range query.Types {
		types[value] = struct{}{}
	}
	i.stateMu.Lock()
	defer i.stateMu.Unlock()
	selected := make([]bridge.Event, 0, limit)
	for _, event := range i.events {
		if compareCursor(event.ID, query.After) <= 0 {
			continue
		}
		if len(types) != 0 {
			if _, ok := types[event.Type]; !ok {
				continue
			}
		}
		selected = append(selected, event)
		if len(selected) == limit {
			break
		}
	}
	return json.Marshal(bridge.EventBatch{Events: selected, Cursor: fmt.Sprint(i.nextEvent)})
}

func capabilities() []bridge.Capability {
	return []bridge.Capability{
		capability("window.navigate", bridge.EffectWrite, "url"),
		capability("window.click", bridge.EffectWrite, "selector"),
		capability("window.fill", bridge.EffectWrite, "selector", "value"),
		capability("window.press", bridge.EffectWrite, "key"),
		capability("window.evaluate", bridge.EffectExternal, "expression"),
		capability("window.screenshot", bridge.EffectRead),
	}
}

func capability(name string, effect bridge.Effect, required ...string) bridge.Capability {
	schema, _ := json.Marshal(map[string]any{"type": "object", "required": required, "additionalProperties": true})
	return bridge.Capability{Name: name, Effect: effect, InputSchema: schema}
}

func capabilityNames() []string {
	values := make([]string, 0, len(capabilities()))
	for _, value := range capabilities() {
		values = append(values, value.Name)
	}
	sort.Strings(values)
	return values
}

func decodeOptions(raw json.RawMessage) (options, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return options{}, nil
	}
	var value options
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return options{}, fmt.Errorf("decode WebDriver options: %w", err)
	}
	return value, nil
}

func validateEndpoint(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("parse WebDriver endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("WebDriver endpoint must use http or https")
	}
	if parsed.Host == "" {
		return "", errors.New("WebDriver endpoint must include a host")
	}
	if parsed.User != nil {
		return "", errors.New("WebDriver endpoint must not contain URL credentials")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func requiredString(input map[string]any, name string) (string, error) {
	value, ok := input[name].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func keyActions(value string) map[string]any {
	return map[string]any{"actions": []any{map[string]any{
		"type": "key", "id": "keyboard", "actions": []any{
			map[string]string{"type": "keyDown", "value": value},
			map[string]string{"type": "keyUp", "value": value},
		},
	}}}
}

func webDriverKey(value string) string {
	switch strings.ToLower(value) {
	case "enter":
		return "\uE007"
	case "tab":
		return "\uE004"
	case "escape":
		return "\uE00C"
	case "backspace":
		return "\uE003"
	default:
		return value
	}
}

func compareCursor(left, right string) int {
	var a, b uint64
	_, _ = fmt.Sscan(left, &a)
	_, _ = fmt.Sscan(right, &b)
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
