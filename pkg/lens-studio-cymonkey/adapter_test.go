package lensstudiocymonkey

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	jangolova "cymonkey/lib/jangolova"
	contract "cymonkey/src/jangolova/contract"
	"cymonkey/src/jangolova/sdk"
)

type staticConnection struct{ headers map[string]string }

func (c staticConnection) Snapshot() sdk.EndpointConnectionSnapshot {
	return sdk.EndpointConnectionSnapshot{Headers: c.headers, Revision: 1}
}
func (staticConnection) Updates() <-chan uint64 { return nil }
func (staticConnection) Acknowledge(uint64)     {}

type toolCall struct {
	Name      string
	Arguments map[string]any
}

type mcpFixture struct {
	t        *testing.T
	mu       sync.Mutex
	calls    []toolCall
	requests int
	server   *httptest.Server
}

func newMCPFixture(t *testing.T) *mcpFixture {
	fixture := &mcpFixture{t: t}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.handle))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *mcpFixture) handle(writer http.ResponseWriter, request *http.Request) {
	if got := request.Header.Get("X-Lens-Studio-Target"); got != "attached" {
		f.t.Errorf("target-owned header = %q, want attached", got)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer fixture-token" {
		f.t.Errorf("authorization = %q, want bearer token", got)
	}
	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
		f.t.Errorf("decode request: %v", err)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.requests++
	f.mu.Unlock()
	if message.Method != "initialize" && request.Header.Get("Mcp-Session-Id") != "fixture-session" {
		f.t.Errorf("session header for %s was not retained", message.Method)
	}
	writer.Header().Set("Content-Type", "application/json")
	if message.Method == "initialize" {
		writer.Header().Set("Mcp-Session-Id", "fixture-session")
	}
	if message.Method == "notifications/initialized" {
		writer.WriteHeader(http.StatusAccepted)
		return
	}
	var result any
	switch message.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": defaultProtocolVersion}
	case "tools/list":
		result = map[string]any{"tools": []map[string]any{
			{"name": "GetLensStudioSceneGraph", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
			{"name": "ListLensStudioAssets", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
			{"name": "SetLensStudioProperty", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"entityId": map[string]any{}, "path": map[string]any{}, "type": map[string]any{}, "value": map[string]any{}}}},
			{"name": "RunAndCollectLogsTool", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
		}}
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			f.t.Errorf("decode tool call: %v", err)
		}
		f.mu.Lock()
		f.calls = append(f.calls, toolCall{Name: params.Name, Arguments: params.Arguments})
		f.mu.Unlock()
		switch params.Name {
		case "GetLensStudioSceneGraph":
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "scene graph"}}, "structuredContent": map[string]any{"root": "root-object"}}
		case "ListLensStudioAssets":
			result = map[string]any{"structuredContent": []string{"asset-a"}}
		case "SetLensStudioProperty":
			result = map[string]any{"structuredContent": map[string]any{"updated": true}}
		case "RunAndCollectLogsTool":
			result = map[string]any{"structuredContent": map[string]any{"logs": []string{"preview ready"}}}
		default:
			result = map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "unknown tool"}}}
		}
	default:
		result = map[string]any{}
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(message.ID), "result": result})
}

func (f *mcpFixture) callNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]string, len(f.calls))
	for index, call := range f.calls {
		result[index] = call.Name
	}
	return result
}

func (f *mcpFixture) latestCall() toolCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func TestLensStudioBackendUsesBoundedToolAliasesAndRollback(t *testing.T) {
	fixture := newMCPFixture(t)
	t.Setenv("LENS_STUDIO_FIXTURE_TOKEN", "fixture-token")
	validated := 0
	host := sdk.Host{
		ValidateEndpoint: func(endpoint sdk.TargetEndpoint) error {
			validated++
			if endpoint.Protocol != Protocol {
				t.Errorf("protocol = %q", endpoint.Protocol)
			}
			return nil
		},
		Redact: func(message string, _ sdk.EngineTarget) string { return message },
	}
	target := sdk.EngineTarget{TargetID: "lens-project", Kind: Runtime, Endpoints: []sdk.TargetEndpoint{{
		Protocol: Protocol, URL: fixture.server.URL, Connection: staticConnection{headers: map[string]string{"X-Lens-Studio-Target": "attached"}},
	}}}
	options := json.RawMessage(`{"bearerTokenEnv":"LENS_STUDIO_FIXTURE_TOKEN","allowedSceneObjectIds":["root-object"],"allowedPropertyPaths":["localTransform.position"]}`)
	engine, err := (Backend{}).Connect(context.Background(), sdk.EngineSpec{Options: options, RequiredCapabilities: []string{ActionSceneList}}, target, jangolova.Options{Host: host})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = engine.Disconnect(context.Background()) })
	if validated < 4 { // connect plus MCP initialization and discovery calls
		t.Fatalf("host validation calls = %d, want repeated validation", validated)
	}
	runtime := engine.(*instance)

	helloRaw, err := runtime.Call(context.Background(), sdk.MethodHello, nil)
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	var hello contract.Hello
	_ = json.Unmarshal(helloRaw, &hello)
	if err := contract.ValidateHello(hello); err != nil {
		t.Fatalf("hello contract: %v", err)
	}
	capabilitiesRaw, err := runtime.Call(context.Background(), sdk.MethodCapabilities, nil)
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	var capabilities []contract.Capability
	_ = json.Unmarshal(capabilitiesRaw, &capabilities)
	if err := contract.ValidateCapabilities(capabilities); err != nil {
		t.Fatalf("capabilities contract: %v", err)
	}
	healthRaw, err := runtime.Call(context.Background(), methodHealth, nil)
	if err != nil || !strings.Contains(string(healthRaw), `"status":"connected"`) {
		t.Fatalf("health = %s, %v", healthRaw, err)
	}

	descriptionRaw, err := runtime.Call(context.Background(), sdk.MethodDescribe, nil)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	var description contract.Description
	_ = json.Unmarshal(descriptionRaw, &description)
	if len(description.Surfaces) != 3 || description.Surfaces[0].Kind != "project" || description.Surfaces[1].Kind != "scene" || description.Surfaces[2].Kind != "scene-object" {
		t.Fatalf("unexpected explicit resources: %#v", description.Surfaces)
	}

	propertyAction := json.RawMessage(`{"name":"scene.property.set","input":{"sceneObjectId":"root-object","propertyPath":"localTransform.position","valueType":"vector3","value":{"x":1,"y":2,"z":3},"previousValue":{"x":0,"y":0,"z":0}}}`)
	propertyResult, err := runtime.Call(context.Background(), sdk.MethodAct, propertyAction)
	if err != nil {
		t.Fatalf("set property: %v", err)
	}
	call := fixture.latestCall()
	if call.Name != "SetLensStudioProperty" || call.Arguments["entityId"] != "root-object" || call.Arguments["path"] != "localTransform.position" || call.Arguments["type"] != "vector3" {
		t.Fatalf("property call was not mapped to the declared Lens Studio tool: %#v", call)
	}
	if !strings.Contains(string(propertyResult), `"rollback"`) || !strings.Contains(string(propertyResult), `"previousValue"`) {
		t.Fatalf("property result lacks rollback: %s", propertyResult)
	}

	if _, err := runtime.Call(context.Background(), sdk.MethodAct, json.RawMessage(`{"name":"preview.run-and-collect-logs","input":{}}`)); err != nil {
		t.Fatalf("preview evidence: %v", err)
	}
	if fixture.latestCall().Name != "RunAndCollectLogsTool" {
		t.Fatalf("preview called %q", fixture.latestCall().Name)
	}

	beforeDenied := len(fixture.callNames())
	deniedAction := json.RawMessage(`{"name":"scene.property.set","input":{"sceneObjectId":"unregistered","propertyPath":"localTransform.position","valueType":"vector3","value":{},"previousValue":{}}}`)
	if _, err := runtime.Call(context.Background(), sdk.MethodAct, deniedAction); err == nil || !strings.Contains(err.Error(), "allowlisted") {
		t.Fatalf("unregistered object error = %v", err)
	}
	if len(fixture.callNames()) != beforeDenied {
		t.Fatal("denied action reached the MCP tool")
	}

	eventsRaw, err := runtime.Call(context.Background(), sdk.MethodEvents, json.RawMessage(`{"limit":10}`))
	if err != nil || !strings.Contains(string(eventsRaw), "scene.property-set") {
		t.Fatalf("events = %s, %v", eventsRaw, err)
	}
}

func TestLensStudioEndpointRequiresLoopbackForHTTP(t *testing.T) {
	if _, err := validateEndpoint("http://editor.example.test/mcp"); err == nil {
		t.Fatal("remote HTTP endpoint was accepted")
	}
	if _, err := validateEndpoint("http://127.0.0.1:8000/mcp"); err != nil {
		t.Fatalf("loopback HTTP endpoint rejected: %v", err)
	}
	if _, err := validateEndpoint("https://editor.example.test/mcp"); err != nil {
		t.Fatalf("HTTPS endpoint rejected: %v", err)
	}
}

func TestLensStudioConnectFailsWithoutPropertyTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var message struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		_ = json.NewDecoder(request.Body).Decode(&message)
		writer.Header().Set("Content-Type", "application/json")
		if message.Method == "initialize" {
			writer.Header().Set("Mcp-Session-Id", "missing-tool")
		}
		if message.Method == "notifications/initialized" {
			writer.WriteHeader(http.StatusAccepted)
			return
		}
		result := any(map[string]any{})
		if message.Method == "initialize" {
			result = map[string]any{"protocolVersion": defaultProtocolVersion}
		}
		if message.Method == "tools/list" {
			result = map[string]any{"tools": []map[string]any{{"name": "GetLensStudioSceneGraph", "inputSchema": map[string]any{"type": "object"}}}}
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(message.ID), "result": result})
	}))
	defer server.Close()
	_, err := (Backend{}).Connect(context.Background(), sdk.EngineSpec{}, sdk.EngineTarget{Kind: Runtime, Endpoints: []sdk.TargetEndpoint{{Protocol: Protocol, URL: server.URL}}}, jangolova.Options{Host: sdk.Host{ValidateEndpoint: func(sdk.TargetEndpoint) error { return nil }}})
	if err == nil || !strings.Contains(err.Error(), "scene-property tool") {
		t.Fatalf("Connect() error = %v", err)
	}
}
