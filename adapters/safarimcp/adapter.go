// Package safarimcp implements a Jangolova interaction adapter for a
// caller-owned Safari MCP server exposed through a Streamable HTTP relay.
// The target provider owns Safari, safaridriver, the stdio relay, and their
// lifecycle; this adapter never launches or terminates any of them.
package safarimcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cymonkey/internal/bridge"
	"cymonkey/internal/manifest"
	"cymonkey/internal/orchestrator"
	"cymonkey/targetconn"
)

const defaultProtocolVersion = "2025-06-18"

type Adapter struct{}

type options struct {
	RequestTimeout  string `json:"requestTimeout,omitempty"`
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	BearerTokenEnv  string `json:"bearerTokenEnv,omitempty"`
}

type tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type instance struct {
	endpoint        string
	protocolVersion string
	sessionID       string
	bearerToken     string
	client          *http.Client
	tools           map[string]tool
	callMu          sync.Mutex
	stateMu         sync.Mutex
	nextID          atomic.Uint64
	disconnected    bool
	events          []bridge.Event
	nextEvent       uint64
	lifecycle       chan orchestrator.EngineEvent
	closeOnce       sync.Once
}

var _ orchestrator.EngineAdapter = Adapter{}
var _ orchestrator.EngineInspector = Adapter{}
var _ orchestrator.EngineInstance = (*instance)(nil)
var _ orchestrator.EngineHealthProvider = (*instance)(nil)
var _ orchestrator.EngineCapabilityProvider = (*instance)(nil)
var _ orchestrator.EngineEventSource = (*instance)(nil)
var _ bridge.Caller = (*instance)(nil)

func (Adapter) InspectEngine(context.Context) orchestrator.EngineInspection {
	return orchestrator.EngineInspection{Available: true, Capabilities: []string{
		"act", "capabilities", "describe", "events", "mcp.call", "target.safari-mcp",
	}}
}

func (Adapter) Connect(
	ctx context.Context,
	spec manifest.EngineSpec,
	target orchestrator.EngineTarget,
) (orchestrator.EngineInstance, error) {
	if target.Kind != "browser" {
		return nil, errors.New("Safari MCP requires target.kind browser")
	}
	endpoint, ok := target.Endpoint("mcp-streamable-http")
	if !ok {
		return nil, errors.New("Safari MCP requires a caller-owned mcp-streamable-http endpoint")
	}
	endpointURL, err := validateEndpoint(endpoint.URL)
	if err != nil {
		return nil, err
	}
	config, err := decodeOptions(spec.Options)
	if err != nil {
		return nil, err
	}
	timeout := 30 * time.Second
	if config.RequestTimeout != "" {
		timeout, err = time.ParseDuration(config.RequestTimeout)
		if err != nil || timeout <= 0 {
			return nil, fmt.Errorf("invalid Safari MCP requestTimeout %q", config.RequestTimeout)
		}
	}
	protocolVersion := strings.TrimSpace(config.ProtocolVersion)
	if protocolVersion == "" {
		protocolVersion = defaultProtocolVersion
	}
	bearerToken := ""
	if name := strings.TrimSpace(config.BearerTokenEnv); name != "" {
		bearerToken = strings.TrimSpace(os.Getenv(name))
		if bearerToken == "" {
			return nil, fmt.Errorf("Safari MCP bearer token environment variable %s is empty", name)
		}
	}
	client, err := targetconn.HTTPClient(endpoint, timeout)
	if err != nil {
		return nil, err
	}
	running := &instance{
		endpoint:        endpointURL,
		protocolVersion: protocolVersion,
		bearerToken:     bearerToken,
		client:          client,
		tools:           make(map[string]tool),
		lifecycle:       make(chan orchestrator.EngineEvent, 1),
	}
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"capabilities"`
	}
	if err := running.rpc(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name": "jangolova-safari-mcp", "version": "0.1.0",
		},
	}, &initialized); err != nil {
		return nil, fmt.Errorf("initialize caller-owned Safari MCP endpoint: %w", err)
	}
	if initialized.ProtocolVersion != "" {
		running.protocolVersion = initialized.ProtocolVersion
	}
	if err := running.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return nil, fmt.Errorf("complete Safari MCP initialization: %w", err)
	}
	if err := running.refreshTools(ctx); err != nil {
		return nil, fmt.Errorf("discover Safari MCP tools: %w", err)
	}
	running.appendEvent("browser.connected", map[string]any{
		"adapter": "safari-mcp", "protocol": "mcp-streamable-http",
	})
	if source := strings.TrimSpace(spec.Source); source != "" {
		params, _ := json.Marshal(map[string]any{
			"name": "window.navigate", "input": map[string]string{"url": source},
		})
		if _, err := running.Call(ctx, bridge.MethodAct, params); err != nil {
			_ = running.Disconnect(context.Background())
			return nil, fmt.Errorf("navigate Safari MCP target: %w", err)
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
	i.client.CloseIdleConnections()
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
	i.callMu.Lock()
	err := i.refreshTools(ctx)
	i.callMu.Unlock()
	if err != nil {
		health.Status = orchestrator.EngineHealthUnhealthy
		health.Message = err.Error()
		return health
	}
	health.Status = orchestrator.EngineHealthHealthy
	return health
}

func (i *instance) Authorize(ctx context.Context, request orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	if strings.TrimSpace(request.Action) == "" {
		return orchestrator.AuthorizeDecision{Authorized: false}, errors.New("Safari interaction action name is required")
	}
	return orchestrator.AuthorizeDecision{Authorized: true}, nil
}

func (i *instance) EngineCapabilities() []string {
	i.stateMu.Lock()
	defer i.stateMu.Unlock()
	return capabilityNames(i.tools)
}

func (i *instance) EngineEvents() <-chan orchestrator.EngineEvent { return i.lifecycle }

func (i *instance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	i.callMu.Lock()
	defer i.callMu.Unlock()
	switch method {
	case bridge.MethodHello:
		return json.Marshal(bridge.Hello{
			ProtocolVersion: bridge.ProtocolVersion,
			Implementation:  bridge.Implementation{Name: "safari-mcp"},
			Features:        []string{"browser", "caller-owned-target", "mcp-tools", "events.local"},
		})
	case bridge.MethodCapabilities:
		i.stateMu.Lock()
		capabilities := capabilities(i.tools)
		i.stateMu.Unlock()
		return json.Marshal(capabilities)
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
	i.stateMu.Lock()
	tools := stableTools(i.tools)
	_, hasPageInfo := i.tools["page_info"]
	i.stateMu.Unlock()
	result := map[string]any{"adapter": "safari-mcp", "tools": tools}
	if hasPageInfo {
		page, err := i.callTool(ctx, "page_info", map[string]any{})
		if err != nil {
			return nil, err
		}
		result["page"] = json.RawMessage(page)
	}
	return json.Marshal(result)
}

func (i *instance) act(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var request struct {
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fmt.Errorf("decode Safari MCP action: %w", err)
	}
	toolName := ""
	arguments := map[string]any{}
	switch request.Name {
	case "window.navigate":
		toolName = "navigate_to_url"
		value, err := requiredString(request.Input, "url")
		if err != nil {
			return nil, err
		}
		arguments["url"] = value
	case "window.evaluate":
		toolName = "evaluate_javascript"
		value, err := requiredString(request.Input, "expression")
		if err != nil {
			return nil, err
		}
		arguments[i.toolArgument(toolName, "script", "expression", "javascript", "code")] = value
	case "window.screenshot":
		toolName = "screenshot"
	case "browser.interact":
		toolName = "page_interactions"
		arguments = request.Input
	case "mcp.call":
		var ok bool
		toolName, ok = request.Input["name"].(string)
		if !ok || toolName == "" {
			return nil, errors.New("mcp.call input.name is required")
		}
		if value, exists := request.Input["arguments"]; exists {
			arguments, ok = value.(map[string]any)
			if !ok {
				return nil, errors.New("mcp.call input.arguments must be an object")
			}
		}
	default:
		const prefix = "mcp.tool."
		if !strings.HasPrefix(request.Name, prefix) {
			return nil, fmt.Errorf("unsupported Safari MCP action %q", request.Name)
		}
		toolName = strings.TrimPrefix(request.Name, prefix)
		arguments = request.Input
	}
	result, err := i.callTool(ctx, toolName, arguments)
	if err != nil {
		return nil, err
	}
	i.appendEvent("browser.action", map[string]any{"name": request.Name, "tool": toolName})
	return result, nil
}

func (i *instance) callTool(ctx context.Context, name string, arguments map[string]any) (json.RawMessage, error) {
	i.stateMu.Lock()
	_, ok := i.tools[name]
	i.stateMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("Safari MCP tool %q is unavailable", name)
	}
	var result json.RawMessage
	if err := i.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments}, &result); err != nil {
		return nil, fmt.Errorf("call Safari MCP tool %s: %w", name, err)
	}
	var status struct {
		IsError bool `json:"isError"`
	}
	_ = json.Unmarshal(result, &status)
	if status.IsError {
		return nil, fmt.Errorf("Safari MCP tool %s failed: %s", name, summarizeToolError(result))
	}
	return result, nil
}

func (i *instance) refreshTools(ctx context.Context) error {
	discovered := make(map[string]tool)
	cursor := ""
	for page := 0; page < 32; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result struct {
			Tools      []tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := i.rpc(ctx, "tools/list", params, &result); err != nil {
			return err
		}
		for _, value := range result.Tools {
			if value.Name != "" {
				discovered[value.Name] = value
			}
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	if len(discovered) == 0 {
		return errors.New("Safari MCP endpoint returned no tools")
	}
	i.stateMu.Lock()
	i.tools = discovered
	i.stateMu.Unlock()
	return nil
}

func (i *instance) rpc(ctx context.Context, method string, params any, destination any) error {
	id := i.nextID.Add(1)
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	response, err := i.post(ctx, method, payload, true)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("MCP error %d: %s", response.Error.Code, response.Error.Message)
	}
	if destination == nil {
		return nil
	}
	if raw, ok := destination.(*json.RawMessage); ok {
		*raw = append((*raw)[:0], response.Result...)
		return nil
	}
	if err := json.Unmarshal(response.Result, destination); err != nil {
		return fmt.Errorf("decode MCP %s result: %w", method, err)
	}
	return nil
}

func (i *instance) notify(ctx context.Context, method string, params any) error {
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	_, err := i.post(ctx, method, payload, false)
	return err
}

func (i *instance) post(ctx context.Context, method string, payload []byte, expectResponse bool) (rpcEnvelope, error) {
	i.stateMu.Lock()
	disconnected := i.disconnected
	sessionID := i.sessionID
	i.stateMu.Unlock()
	if disconnected {
		return rpcEnvelope{}, errors.New("Safari MCP interaction is disconnected")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, i.endpoint, bytes.NewReader(payload))
	if err != nil {
		return rpcEnvelope{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Mcp-Method", method)
	if method == "tools/call" {
		var message struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.Unmarshal(payload, &message) == nil && message.Params.Name != "" {
			request.Header.Set("Mcp-Name", message.Params.Name)
		}
	}
	if method != "initialize" {
		request.Header.Set("MCP-Protocol-Version", i.protocolVersion)
	}
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	if i.bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+i.bearerToken)
	}
	response, err := i.client.Do(request)
	if err != nil {
		return rpcEnvelope{}, err
	}
	defer response.Body.Close()
	if value := strings.TrimSpace(response.Header.Get("Mcp-Session-Id")); value != "" {
		i.stateMu.Lock()
		i.sessionID = value
		i.stateMu.Unlock()
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		contents, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
		return rpcEnvelope{}, fmt.Errorf("MCP HTTP %s: %s", response.Status, strings.TrimSpace(string(contents)))
	}
	if !expectResponse {
		return rpcEnvelope{}, nil
	}
	contentType := response.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		envelope, decodeErr := decodeSSEReader(response.Body)
		if decodeErr != nil {
			return rpcEnvelope{}, fmt.Errorf("decode MCP response: %w", decodeErr)
		}
		return envelope, nil
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return rpcEnvelope{}, err
	}
	if len(bytes.TrimSpace(contents)) == 0 {
		return rpcEnvelope{}, errors.New("MCP returned an empty response")
	}
	var envelope rpcEnvelope
	err = json.Unmarshal(contents, &envelope)
	if err != nil {
		return rpcEnvelope{}, fmt.Errorf("decode MCP response: %w", err)
	}
	return envelope, nil
}

func decodeSSE(contents []byte) (rpcEnvelope, error) {
	return decodeSSEReader(bytes.NewReader(contents))
}

func decodeSSEReader(contents io.Reader) (rpcEnvelope, error) {
	scanner := bufio.NewScanner(contents)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var envelope rpcEnvelope
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &envelope); err == nil && (len(envelope.Result) != 0 || envelope.Error != nil) {
			return envelope, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return rpcEnvelope{}, err
	}
	return rpcEnvelope{}, errors.New("SSE response contained no JSON-RPC result")
}

func capabilities(tools map[string]tool) []bridge.Capability {
	values := []bridge.Capability{capability("mcp.call", bridge.EffectExternal, objectSchema("name"))}
	for _, value := range stableTools(tools) {
		values = append(values, capability("mcp.tool."+value.Name, toolEffect(value.Name), value.InputSchema))
	}
	if _, ok := tools["navigate_to_url"]; ok {
		values = append(values, capability("window.navigate", bridge.EffectWrite, objectSchema("url")))
	}
	if _, ok := tools["evaluate_javascript"]; ok {
		values = append(values, capability("window.evaluate", bridge.EffectExternal, objectSchema("expression")))
	}
	if _, ok := tools["screenshot"]; ok {
		values = append(values, capability("window.screenshot", bridge.EffectRead, objectSchema()))
	}
	if value, ok := tools["page_interactions"]; ok {
		values = append(values, capability("browser.interact", bridge.EffectWrite, value.InputSchema))
	}
	sort.Slice(values, func(left, right int) bool { return values[left].Name < values[right].Name })
	return values
}

func capabilityNames(tools map[string]tool) []string {
	values := []string{"act", "capabilities", "describe", "events", "target.safari-mcp"}
	for _, value := range capabilities(tools) {
		values = append(values, value.Name)
	}
	sort.Strings(values)
	return values
}

func capability(name string, effect bridge.Effect, schema json.RawMessage) bridge.Capability {
	if !json.Valid(schema) {
		schema = objectSchema()
	}
	return bridge.Capability{Name: name, Effect: effect, InputSchema: schema}
}

func objectSchema(required ...string) json.RawMessage {
	value, _ := json.Marshal(map[string]any{"type": "object", "required": required, "additionalProperties": true})
	return value
}

func toolEffect(name string) bridge.Effect {
	if strings.Contains(name, "evaluate") || strings.Contains(name, "network") || strings.Contains(name, "console") {
		return bridge.EffectExternal
	}
	for _, prefix := range []string{"get_", "list_", "page_info", "screenshot", "browser_console"} {
		if strings.HasPrefix(name, prefix) {
			return bridge.EffectRead
		}
	}
	return bridge.EffectWrite
}

func stableTools(values map[string]tool) []tool {
	result := make([]tool, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}

func (i *instance) toolArgument(name string, candidates ...string) string {
	i.stateMu.Lock()
	value := i.tools[name]
	i.stateMu.Unlock()
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	_ = json.Unmarshal(value.InputSchema, &schema)
	for _, candidate := range candidates {
		if _, ok := schema.Properties[candidate]; ok {
			return candidate
		}
	}
	return candidates[0]
}

func decodeOptions(raw json.RawMessage) (options, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return options{}, nil
	}
	var value options
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return options{}, fmt.Errorf("decode Safari MCP options: %w", err)
	}
	return value, nil
}

func validateEndpoint(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("parse Safari MCP endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("Safari MCP Streamable HTTP endpoint must use http or https")
	}
	if parsed.Host == "" {
		return "", errors.New("Safari MCP endpoint must include a host")
	}
	if parsed.User != nil {
		return "", errors.New("Safari MCP endpoint must not contain URL credentials")
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func requiredString(input map[string]any, name string) (string, error) {
	value, ok := input[name].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func summarizeToolError(raw json.RawMessage) string {
	var value struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(raw, &value)
	for _, content := range value.Content {
		if content.Type == "text" && content.Text != "" {
			return content.Text
		}
	}
	return string(raw)
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
