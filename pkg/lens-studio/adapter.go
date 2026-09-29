// Package lensstudiocymonkey exposes a deliberately small Lens Studio editor
// bridge. It attaches to the MCP server that Lens Studio already owns; it
// never launches Lens Studio, starts a second editor server, or accepts an
// unbounded tool name from an agent.
package lensstudiocymonkey

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	jangolova "jangolova"
	contract "jangolova/contract"
	"jangolova/sdk"
)

const (
	// Runtime is the target runtime advertised by this integration.
	Runtime = "lens-studio"
	// Protocol is Lens Studio's caller-owned Streamable HTTP MCP endpoint.
	Protocol = "mcp-streamable-http"

	ActionProjectDescribe       = "project.describe"
	ActionSceneList             = "scene.list"
	ActionScenePropertySet      = "scene.property.set"
	ActionPreviewRunCollectLogs = "preview.run-and-collect-logs"

	defaultProtocolVersion = "2025-06-18"
	methodHealth           = "health"
)

var (
	sceneGraphTools       = []string{"GetLensStudioSceneGraph", "get_lens_studio_scene_graph", "get_scene_graph"}
	assetListTools        = []string{"ListLensStudioAssets", "list_lens_studio_assets", "list_assets"}
	setPropertyTools      = []string{"SetLensStudioProperty", "set_lens_studio_property", "set_property"}
	previewLogTools       = []string{"RunAndCollectLogsTool", "run_and_collect_logs", "run_preview_and_collect_logs"}
	allowedReadActions    = map[string]struct{}{ActionProjectDescribe: {}, ActionSceneList: {}}
	allowedWriteActions   = map[string]struct{}{ActionScenePropertySet: {}, ActionPreviewRunCollectLogs: {}}
	validPropertyValueTyp = map[string]struct{}{"boolean": {}, "number": {}, "string": {}, "vector2": {}, "vector3": {}, "vector4": {}, "color": {}, "reference": {}}
)

// Options must be supplied by the target owner in EngineSpec.Options. Write
// access is denied unless both allowlists are explicitly present. Jangolova's
// normal action-approval policy remains a second, independent gate.
type Options struct {
	RequestTimeout       string   `json:"requestTimeout,omitempty"`
	ProtocolVersion      string   `json:"protocolVersion,omitempty"`
	BearerTokenEnv       string   `json:"bearerTokenEnv,omitempty"`
	AllowedSceneObjectID []string `json:"allowedSceneObjectIds,omitempty"`
	AllowedPropertyPath  []string `json:"allowedPropertyPaths,omitempty"`
}

type tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Backend is a reviewed external Jangolova module. The host explicitly mounts
// it through jangolova.Adapter.Backends; this package never self-registers.
type Backend struct{}

type instance struct {
	host            sdk.Host
	target          sdk.EngineTarget
	endpointRef     sdk.TargetEndpoint
	endpoint        string
	protocolVersion string
	bearerToken     string
	client          *http.Client
	options         Options

	callMu    sync.Mutex
	stateMu   sync.Mutex
	sessionID string
	tools     map[string]tool
	events    []sdkEvent
	nextEvent uint64
	nextID    atomic.Uint64
	disconn   bool
	lifecycle chan sdk.EngineEvent
	closeOnce sync.Once
}

var (
	_ jangolova.Backend            = Backend{}
	_ sdk.EngineInstance           = (*instance)(nil)
	_ sdk.EngineHealthProvider     = (*instance)(nil)
	_ sdk.EngineCapabilityProvider = (*instance)(nil)
	_ sdk.EngineEventSource        = (*instance)(nil)
	_ sdk.Caller                   = (*instance)(nil)
)

type sdkEvent struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurredAt"`
	Data       json.RawMessage `json:"data,omitempty"`
}

func (Backend) Name() jangolova.BackendName { return jangolova.BackendName("lens-studio-mcp") }

func (Backend) Domains() []contract.Domain { return []contract.Domain{contract.DomainViewer} }

func (Backend) Compatible(target sdk.EngineTarget) bool {
	if target.Kind != Runtime {
		return false
	}
	_, ok := target.Endpoint(Protocol)
	return ok
}

func (Backend) Connect(ctx context.Context, spec sdk.EngineSpec, target sdk.EngineTarget, config jangolova.Options) (sdk.EngineInstance, error) {
	if !(Backend{}).Compatible(target) {
		return nil, fmt.Errorf("Lens Studio requires target.kind %q with a caller-owned %s endpoint", Runtime, Protocol)
	}
	endpoint, _ := target.Endpoint(Protocol)
	if err := config.Host.Validate(endpoint); err != nil {
		return nil, err
	}
	endpointURL, err := validateEndpoint(endpoint.URL)
	if err != nil {
		return nil, err
	}
	options, err := decodeOptions(spec.Options)
	if err != nil {
		return nil, err
	}
	timeout := 30 * time.Second
	if options.RequestTimeout != "" {
		timeout, err = time.ParseDuration(options.RequestTimeout)
		if err != nil || timeout <= 0 {
			return nil, fmt.Errorf("invalid Lens Studio requestTimeout %q", options.RequestTimeout)
		}
	}
	bearerToken := ""
	if name := strings.TrimSpace(options.BearerTokenEnv); name != "" {
		bearerToken = strings.TrimSpace(os.Getenv(name))
		if bearerToken == "" {
			return nil, fmt.Errorf("Lens Studio bearer token environment variable %s is empty", name)
		}
	}
	protocolVersion := strings.TrimSpace(options.ProtocolVersion)
	if protocolVersion == "" {
		protocolVersion = defaultProtocolVersion
	}
	running := &instance{
		endpoint: endpointURL, protocolVersion: protocolVersion, bearerToken: bearerToken, client: &http.Client{Timeout: timeout},
		host: config.Host, target: target, endpointRef: endpoint,
		options: options, tools: make(map[string]tool), lifecycle: make(chan sdk.EngineEvent, 1),
	}
	return connect(ctx, spec, running)
}

func connect(ctx context.Context, spec sdk.EngineSpec, running *instance) (sdk.EngineInstance, error) {
	return connectLensStudio(ctx, spec, running)
}

func connectLensStudio(ctx context.Context, spec sdk.EngineSpec, running *instance) (sdk.EngineInstance, error) {
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := running.rpc(ctx, "initialize", map[string]any{
		"protocolVersion": running.protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "jangolova-lens-studio", "version": "0.1.0"},
	}, &initialized); err != nil {
		return nil, fmt.Errorf("initialize caller-owned Lens Studio MCP endpoint: %w", err)
	}
	if initialized.ProtocolVersion != "" {
		running.protocolVersion = initialized.ProtocolVersion
	}
	if err := running.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return nil, fmt.Errorf("complete Lens Studio MCP initialization: %w", err)
	}
	if err := running.refreshTools(ctx); err != nil {
		return nil, fmt.Errorf("discover Lens Studio MCP tools: %w", err)
	}
	if running.lookupTool(sceneGraphTools...) == "" {
		return nil, errors.New("Lens Studio MCP endpoint does not expose a scene-graph tool")
	}
	if running.lookupTool(setPropertyTools...) == "" {
		return nil, errors.New("Lens Studio MCP endpoint does not expose a scene-property tool")
	}
	if missing := missingCapabilities(spec.RequiredCapabilities, running.EngineCapabilities()); len(missing) != 0 {
		return nil, fmt.Errorf("Lens Studio MCP endpoint is missing required capabilities: %s", strings.Join(missing, ", "))
	}
	running.appendEvent("lens-studio.connected", map[string]any{"protocol": Protocol})
	return running, nil
}

func missingCapabilities(required, offered []string) []string {
	missing := make([]string, 0)
	for _, expected := range required {
		if !contains(offered, expected) {
			missing = append(missing, expected)
		}
	}
	return missing
}

// InspectEngine is supplied by the host-facing adapter rather than this
// backend, which is an explicitly registered external module.
func InspectEngine(context.Context) sdk.EngineInspection {
	return sdk.EngineInspection{Available: true, Capabilities: []string{
		"act", "capabilities", "describe", "events", "health", "target.lens-studio-mcp",
		ActionProjectDescribe, ActionSceneList, ActionScenePropertySet, ActionPreviewRunCollectLogs,
	}}
}

func (i *instance) Disconnect(context.Context) error {
	i.stateMu.Lock()
	if i.disconn {
		i.stateMu.Unlock()
		return nil
	}
	i.disconn = true
	i.stateMu.Unlock()
	i.client.CloseIdleConnections()
	i.closeOnce.Do(func() {
		i.lifecycle <- sdk.EngineEvent{Type: "interaction.disconnected", Status: sdk.EngineHealthStopped, OccurredAt: time.Now().UTC()}
		close(i.lifecycle)
	})
	return nil
}

func (i *instance) Authorize(_ context.Context, request sdk.AuthorizeRequest) (sdk.AuthorizeDecision, error) {
	if _, ok := allowedReadActions[request.Action]; ok {
		return sdk.AuthorizeDecision{Authorized: true}, nil
	}
	if _, ok := allowedWriteActions[request.Action]; !ok {
		return sdk.AuthorizeDecision{Reason: "Lens Studio action is not exposed by this bridge"}, nil
	}
	if request.Action == ActionPreviewRunCollectLogs {
		if i.lookupTool(previewLogTools...) == "" {
			return sdk.AuthorizeDecision{Reason: "Lens Studio preview-log tool is unavailable"}, nil
		}
		return sdk.AuthorizeDecision{Authorized: true}, nil
	}
	input, err := decodePropertyInput(request.Input)
	if err != nil {
		return sdk.AuthorizeDecision{Reason: err.Error()}, nil
	}
	if !contains(i.options.AllowedSceneObjectID, input.SceneObjectID) {
		return sdk.AuthorizeDecision{Reason: "scene object is not allowlisted"}, nil
	}
	if !contains(i.options.AllowedPropertyPath, input.PropertyPath) {
		return sdk.AuthorizeDecision{Reason: "scene property path is not allowlisted"}, nil
	}
	return sdk.AuthorizeDecision{Authorized: true}, nil
}

func (i *instance) EngineHealth(ctx context.Context) sdk.EngineHealth {
	i.callMu.Lock()
	err := i.refreshTools(ctx)
	i.callMu.Unlock()
	if err != nil {
		return sdk.EngineHealth{Status: sdk.EngineHealthUnhealthy, Message: err.Error(), ObservedAt: time.Now().UTC()}
	}
	return sdk.EngineHealth{Status: sdk.EngineHealthHealthy, ObservedAt: time.Now().UTC()}
}

func (i *instance) EngineCapabilities() []string {
	values := []string{"act", "capabilities", "describe", "events", "health", "target.lens-studio-mcp", ActionProjectDescribe, ActionSceneList, ActionScenePropertySet}
	if i.lookupTool(previewLogTools...) != "" {
		values = append(values, ActionPreviewRunCollectLogs)
	}
	sort.Strings(values)
	return values
}

func (i *instance) EngineEvents() <-chan sdk.EngineEvent { return i.lifecycle }

func (i *instance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	i.callMu.Lock()
	defer i.callMu.Unlock()
	switch method {
	case sdk.MethodHello:
		return json.Marshal(contract.Hello{ProtocolVersion: contract.ProtocolVersion, Implementation: contract.Implementation{Name: "jangolova-lens-studio", Version: "0.1.0"}, Domains: []contract.Domain{contract.DomainViewer}, Runtimes: []string{Runtime}, Drivers: []contract.Driver{contract.Driver("lens-studio-mcp")}, Features: []string{"caller-owned-target", "mcp", "resources.explicit-allowlist", "rollback"}})
	case sdk.MethodCapabilities:
		return json.Marshal(i.capabilities())
	case sdk.MethodDescribe:
		return i.projectDescribe(ctx)
	case sdk.MethodAct:
		return i.act(ctx, params)
	case sdk.MethodEvents:
		return i.readEvents(params)
	case methodHealth:
		health := sdk.EngineHealth{Status: sdk.EngineHealthHealthy, ObservedAt: time.Now().UTC()}
		if err := i.refreshTools(ctx); err != nil {
			health = sdk.EngineHealth{Status: sdk.EngineHealthUnhealthy, Message: err.Error(), ObservedAt: time.Now().UTC()}
		}
		return json.Marshal(map[string]any{"status": health.Status, "message": health.Message, "observedAt": health.ObservedAt})
	default:
		return nil, fmt.Errorf("unsupported Lens Studio interaction method %q", method)
	}
}

func (i *instance) capabilities() []contract.Capability {
	result := []contract.Capability{
		{Name: ActionProjectDescribe, Description: "Describe the explicitly attached Lens Studio project using its scene graph and, when available, asset list.", Domain: contract.DomainViewer, Runtime: Runtime, Driver: contract.Driver("lens-studio-mcp"), Support: contract.SupportMapped, Lifetime: contract.LifetimeAttachment, Persistence: contract.PersistenceSession, Effect: sdk.EffectRead, ResourceKinds: []string{"project", "scene"}, InputSchema: objectSchema()},
		{Name: ActionSceneList, Description: "List the current Lens Studio scene graph.", Domain: contract.DomainViewer, Runtime: Runtime, Driver: contract.Driver("lens-studio-mcp"), Support: contract.SupportMapped, Lifetime: contract.LifetimeAttachment, Persistence: contract.PersistenceSession, Effect: sdk.EffectRead, ResourceKinds: []string{"scene"}, InputSchema: objectSchema()},
		{Name: ActionScenePropertySet, Description: "Set one allowlisted scene property. previousValue is required so the result contains a safe rollback request.", Domain: contract.DomainViewer, Runtime: Runtime, Driver: contract.Driver("lens-studio-mcp"), Support: contract.SupportMapped, Lifetime: contract.LifetimeAttachment, Persistence: contract.PersistenceSession, Effect: sdk.EffectWrite, ResourceKinds: []string{"scene", "scene-object"}, InputSchema: propertySetSchema()},
	}
	if i.lookupTool(previewLogTools...) != "" {
		result = append(result, contract.Capability{Name: ActionPreviewRunCollectLogs, Description: "Run the editor preview and collect Lens Studio log evidence. This bridge does not claim screenshot capture.", Domain: contract.DomainViewer, Runtime: Runtime, Driver: contract.Driver("lens-studio-mcp"), Support: contract.SupportMapped, Lifetime: contract.LifetimeCall, Persistence: contract.PersistenceEphemeral, Effect: sdk.EffectWrite, ResourceKinds: []string{"preview", "log"}, InputSchema: objectSchema()})
	}
	return result
}

func (i *instance) projectDescribe(ctx context.Context) (json.RawMessage, error) {
	sceneTool := i.lookupTool(sceneGraphTools...)
	sceneGraph, err := i.callTool(ctx, sceneTool, map[string]any{})
	if err != nil {
		return nil, err
	}
	projectProperties := map[string]any{"evidence": []map[string]any{{"tool": sceneTool, "result": json.RawMessage(sceneGraph)}}}
	if assetTool := i.lookupTool(assetListTools...); assetTool != "" {
		assets, assetErr := i.callTool(ctx, assetTool, map[string]any{})
		if assetErr != nil {
			return nil, assetErr
		}
		projectProperties["assets"] = json.RawMessage(assets)
		projectProperties["evidence"] = append(projectProperties["evidence"].([]map[string]any), map[string]any{"tool": assetTool, "result": json.RawMessage(assets)})
	}
	projectID := strings.TrimSpace(i.target.TargetID)
	if projectID == "" {
		projectID = "lens-studio-project"
	}
	projectRaw, _ := json.Marshal(projectProperties)
	surfaces := []contract.Surface{
		{ID: projectID, Domain: contract.DomainViewer, Runtime: Runtime, Kind: "project", Label: "Attached Lens Studio project", Properties: projectRaw},
		{ID: projectID + ":scene", Domain: contract.DomainViewer, Runtime: Runtime, Kind: "scene", Label: "Current Lens Studio scene", Properties: json.RawMessage(sceneGraph)},
	}
	for _, objectID := range i.options.AllowedSceneObjectID {
		if objectID = strings.TrimSpace(objectID); objectID != "" {
			properties, _ := json.Marshal(map[string]any{"allowlisted": true})
			surfaces = append(surfaces, contract.Surface{ID: projectID + ":scene-object:" + objectID, Domain: contract.DomainViewer, Runtime: Runtime, Kind: "scene-object", Label: objectID, Properties: properties})
		}
	}
	i.stateMu.Lock()
	sessionID := i.sessionID
	i.stateMu.Unlock()
	if sessionID == "" {
		sessionID = "attached"
	}
	return json.Marshal(contract.Description{Revision: fmt.Sprintf("mcp-session:%s", sessionID), Surfaces: surfaces, Augmentations: []contract.AugmentationSummary{}})
}

func (i *instance) act(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var request struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fmt.Errorf("decode Lens Studio action: %w", err)
	}
	if len(request.Input) == 0 {
		request.Input = json.RawMessage(`{}`)
	}
	switch request.Name {
	case ActionProjectDescribe:
		result, err := i.projectDescribe(ctx)
		if err == nil {
			i.appendEvent("lens-studio.project.described", map[string]any{})
		}
		return result, err
	case ActionSceneList:
		result, err := i.callTool(ctx, i.lookupTool(sceneGraphTools...), map[string]any{})
		if err != nil {
			return nil, err
		}
		i.appendEvent("lens-studio.scene.listed", map[string]any{})
		return json.Marshal(map[string]any{"sceneGraph": json.RawMessage(result), "evidence": map[string]any{"tool": i.lookupTool(sceneGraphTools...), "result": json.RawMessage(result)}})
	case ActionScenePropertySet:
		return i.setSceneProperty(ctx, request.Input)
	case ActionPreviewRunCollectLogs:
		return i.runPreview(ctx)
	default:
		return nil, fmt.Errorf("Lens Studio action %q is not exposed by this bridge", request.Name)
	}
}

type propertyInput struct {
	SceneObjectID string          `json:"sceneObjectId"`
	PropertyPath  string          `json:"propertyPath"`
	ValueType     string          `json:"valueType"`
	Value         json.RawMessage `json:"value"`
	PreviousValue json.RawMessage `json:"previousValue"`
}

func decodePropertyInput(raw json.RawMessage) (propertyInput, error) {
	var input propertyInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return input, errors.New("scene.property.set input must be an object")
	}
	if strings.TrimSpace(input.SceneObjectID) == "" || strings.TrimSpace(input.PropertyPath) == "" {
		return input, errors.New("scene.property.set requires sceneObjectId and propertyPath")
	}
	if _, ok := validPropertyValueTyp[input.ValueType]; !ok {
		return input, errors.New("scene.property.set valueType is unsupported")
	}
	if !json.Valid(input.Value) || !json.Valid(input.PreviousValue) {
		return input, errors.New("scene.property.set requires JSON value and previousValue")
	}
	return input, nil
}

func (i *instance) setSceneProperty(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	input, err := decodePropertyInput(raw)
	if err != nil {
		return nil, err
	}
	// Call() can be used without Engine Provider, so enforce the same bound here
	// as Authorize() instead of relying on a caller to have run it first.
	decision, err := i.Authorize(ctx, sdk.AuthorizeRequest{TargetID: i.target.TargetID, Action: ActionScenePropertySet, Input: raw})
	if err != nil {
		return nil, err
	}
	if !decision.Authorized {
		return nil, errors.New(decision.Reason)
	}
	toolName := i.lookupTool(setPropertyTools...)
	arguments := i.propertyArguments(toolName, input)
	result, err := i.callTool(ctx, toolName, arguments)
	if err != nil {
		return nil, err
	}
	rollback := map[string]any{
		"name": ActionScenePropertySet,
		"input": map[string]any{
			"sceneObjectId": input.SceneObjectID,
			"propertyPath":  input.PropertyPath,
			"valueType":     input.ValueType,
			"value":         json.RawMessage(input.PreviousValue),
			"previousValue": json.RawMessage(input.Value),
		},
	}
	i.appendEvent("lens-studio.scene.property-set", map[string]any{"sceneObjectId": input.SceneObjectID, "propertyPath": input.PropertyPath})
	return json.Marshal(map[string]any{"changed": true, "tool": toolName, "toolResult": json.RawMessage(result), "rollback": rollback})
}

func (i *instance) runPreview(ctx context.Context) (json.RawMessage, error) {
	decision, err := i.Authorize(ctx, sdk.AuthorizeRequest{TargetID: i.target.TargetID, Action: ActionPreviewRunCollectLogs})
	if err != nil {
		return nil, err
	}
	if !decision.Authorized {
		return nil, errors.New(decision.Reason)
	}
	toolName := i.lookupTool(previewLogTools...)
	if toolName == "" {
		return nil, errors.New("Lens Studio MCP endpoint does not expose preview-log evidence")
	}
	result, err := i.callTool(ctx, toolName, map[string]any{})
	if err != nil {
		return nil, err
	}
	i.appendEvent("lens-studio.preview.logs-collected", map[string]any{})
	return json.Marshal(map[string]any{"evidence": map[string]any{"kind": "preview-logs", "tool": toolName, "result": json.RawMessage(result)}})
}

func (i *instance) propertyArguments(toolName string, input propertyInput) map[string]any {
	return map[string]any{
		i.toolArgument(toolName, "sceneObjectId", "objectId", "entityId", "id"): input.SceneObjectID,
		i.toolArgument(toolName, "propertyPath", "path"):                        input.PropertyPath,
		i.toolArgument(toolName, "valueType", "type"):                           input.ValueType,
		i.toolArgument(toolName, "value"):                                       json.RawMessage(input.Value),
	}
}

func (i *instance) callTool(ctx context.Context, name string, arguments map[string]any) (json.RawMessage, error) {
	if name == "" {
		return nil, errors.New("Lens Studio MCP tool is unavailable")
	}
	i.stateMu.Lock()
	_, exists := i.tools[name]
	i.stateMu.Unlock()
	if !exists {
		return nil, fmt.Errorf("Lens Studio MCP tool %q is unavailable", name)
	}
	var result json.RawMessage
	if err := i.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments}, &result); err != nil {
		return nil, fmt.Errorf("call Lens Studio MCP tool %s: %w", name, err)
	}
	var status struct {
		IsError bool `json:"isError"`
	}
	_ = json.Unmarshal(result, &status)
	if status.IsError {
		return nil, fmt.Errorf("Lens Studio MCP tool %s failed: %s", name, summarizeToolError(result))
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
			if strings.TrimSpace(value.Name) != "" {
				discovered[value.Name] = value
			}
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	if len(discovered) == 0 {
		return errors.New("Lens Studio MCP endpoint returned no tools")
	}
	i.stateMu.Lock()
	i.tools = discovered
	i.stateMu.Unlock()
	return nil
}

func (i *instance) lookupTool(candidates ...string) string {
	i.stateMu.Lock()
	defer i.stateMu.Unlock()
	for _, candidate := range candidates {
		if _, ok := i.tools[candidate]; ok {
			return candidate
		}
	}
	return ""
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

func (i *instance) rpc(ctx context.Context, method string, params any, destination any) error {
	id := i.nextID.Add(1)
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	response, err := i.post(ctx, method, payload, true)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("MCP error %d: %s", response.Error.Code, response.Error.Message)
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
	disconnected, sessionID := i.disconn, i.sessionID
	i.stateMu.Unlock()
	if disconnected {
		return rpcEnvelope{}, errors.New("Lens Studio interaction is disconnected")
	}
	if err := i.host.Validate(i.endpointRef); err != nil {
		return rpcEnvelope{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, i.endpoint, bytes.NewReader(payload))
	if err != nil {
		return rpcEnvelope{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Mcp-Method", method)
	if method != "initialize" {
		request.Header.Set("MCP-Protocol-Version", i.protocolVersion)
	}
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	for name, value := range i.endpointRef.Snapshot().Headers {
		request.Header.Set(name, value)
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
		contents, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return rpcEnvelope{}, errors.New(i.host.RedactString(fmt.Sprintf("Lens Studio MCP HTTP %s: %s", response.Status, strings.TrimSpace(string(contents))), i.target))
	}
	if !expectResponse {
		return rpcEnvelope{}, nil
	}
	if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		return decodeSSE(response.Body)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return rpcEnvelope{}, err
	}
	if len(bytes.TrimSpace(contents)) == 0 {
		return rpcEnvelope{}, errors.New("Lens Studio MCP returned an empty response")
	}
	var envelope rpcEnvelope
	if err := json.Unmarshal(contents, &envelope); err != nil {
		return rpcEnvelope{}, fmt.Errorf("decode Lens Studio MCP response: %w", err)
	}
	return envelope, nil
}

func decodeSSE(contents io.Reader) (rpcEnvelope, error) {
	scanner := bufio.NewScanner(contents)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var envelope rpcEnvelope
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &envelope) == nil && (len(envelope.Result) != 0 || envelope.Error != nil) {
			return envelope, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return rpcEnvelope{}, err
	}
	return rpcEnvelope{}, errors.New("Lens Studio MCP SSE response contained no JSON-RPC result")
}

func (i *instance) appendEvent(eventType string, data any) {
	i.stateMu.Lock()
	defer i.stateMu.Unlock()
	i.nextEvent++
	raw, _ := json.Marshal(data)
	i.events = append(i.events, sdkEvent{ID: fmt.Sprint(i.nextEvent), Type: eventType, OccurredAt: time.Now().UTC(), Data: raw})
	if len(i.events) > 256 {
		i.events = append([]sdkEvent(nil), i.events[len(i.events)-256:]...)
	}
}

func (i *instance) readEvents(raw json.RawMessage) (json.RawMessage, error) {
	var query struct {
		After string   `json:"after"`
		Types []string `json:"types"`
		Limit int      `json:"limit"`
	}
	if len(raw) != 0 && json.Unmarshal(raw, &query) != nil {
		return nil, errors.New("invalid Lens Studio event query")
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
	selected := make([]sdkEvent, 0, limit)
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
	return json.Marshal(struct {
		Events []sdkEvent `json:"events"`
		Cursor string     `json:"cursor"`
	}{Events: selected, Cursor: fmt.Sprint(i.nextEvent)})
}

func decodeOptions(raw json.RawMessage) (Options, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return Options{}, nil
	}
	var options Options
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&options); err != nil {
		return Options{}, fmt.Errorf("decode Lens Studio options: %w", err)
	}
	return options, nil
}

func validateEndpoint(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("Lens Studio MCP endpoint must be an HTTP(S) URL with a host")
	}
	if parsed.User != nil {
		return "", errors.New("Lens Studio MCP endpoint must not contain URL credentials")
	}
	host := strings.Trim(strings.ToLower(parsed.Hostname()), "[]")
	if parsed.Scheme == "http" && host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback()) {
		return "", errors.New("Lens Studio HTTP MCP endpoint must use a loopback host; use HTTPS for a remote endpoint")
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func objectSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false}`)
}

func propertySetSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["sceneObjectId","propertyPath","valueType","value","previousValue"],"properties":{"sceneObjectId":{"type":"string"},"propertyPath":{"type":"string"},"valueType":{"type":"string","enum":["boolean","number","string","vector2","vector3","vector4","color","reference"]},"value":{},"previousValue":{}}}`)
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
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
