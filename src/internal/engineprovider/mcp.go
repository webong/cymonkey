package engineprovider

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// MCPProtocolVersion is the direct Jangolova tool-server protocol revision.
const MCPProtocolVersion = "2025-11-25"

// MCPServer exposes the authenticated Engine Provider as agent-callable tools.
// It never plans, calls a model, or retains agent sessions.
type MCPServer struct {
	service  *Service
	mu       sync.Mutex
	sessions map[string]struct{}
}

func NewMCPServer(service *Service) (*MCPServer, error) {
	if service == nil {
		return nil, errors.New("Jangolova engine provider is required")
	}
	return &MCPServer{service: service, sessions: map[string]struct{}{}}, nil
}

func (server *MCPServer) Routes() http.Handler {
	return server.service.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		server.handleHTTP(w, r)
	}))
}

func (server *MCPServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMCPError(w, http.StatusMethodNotAllowed, nil, -32600, "MCP supports POST requests")
		return
	}
	if contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); contentType != "application/json" {
		writeMCPError(w, http.StatusUnsupportedMediaType, nil, -32600, "MCP requests must use application/json")
		return
	}
	request, err := decodeMCPRequest(r.Body)
	if err != nil {
		writeMCPError(w, http.StatusBadRequest, nil, -32700, "parse error")
		return
	}
	if request.Method == "initialize" {
		if r.Header.Get("Mcp-Session-Id") != "" {
			writeMCPError(w, http.StatusBadRequest, request.ID, -32600, "initialize must not include Mcp-Session-Id")
			return
		}
		sessionID := newMCPSessionID()
		server.mu.Lock()
		server.sessions[sessionID] = struct{}{}
		server.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", sessionID)
		writeJSON(w, http.StatusOK, server.dispatch(r.Context(), request))
		return
	}
	server.mu.Lock()
	_, known := server.sessions[strings.TrimSpace(r.Header.Get("Mcp-Session-Id"))]
	server.mu.Unlock()
	if !known {
		writeMCPError(w, http.StatusNotFound, request.ID, -32000, "unknown MCP session")
		return
	}
	response := server.dispatch(r.Context(), request)
	if response == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (server *MCPServer) ServeStdio(ctx context.Context, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var request mcpRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			if err := encoder.Encode(mcpErrorResponse(nil, -32700, "parse error")); err != nil {
				return err
			}
			continue
		}
		if response := server.dispatch(ctx, request); response != nil {
			if err := encoder.Encode(response); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func mcpErrorResponse(id json.RawMessage, code int, message string) mcpResponse {
	return mcpResponse{JSONRPC: "2.0", ID: id, Error: &mcpRPCError{Code: code, Message: message}}
}

func writeMCPError(w http.ResponseWriter, status int, id json.RawMessage, code int, message string) {
	writeJSON(w, status, mcpErrorResponse(id, code, message))
}

func decodeMCPRequest(body io.Reader) (mcpRequest, error) {
	var request mcpRequest
	decoder := json.NewDecoder(io.LimitReader(body, 2*1024*1024))
	if err := decoder.Decode(&request); err != nil {
		return mcpRequest{}, err
	}
	if request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" {
		return mcpRequest{}, errors.New("invalid JSON-RPC request")
	}
	return request, nil
}

func (server *MCPServer) dispatch(ctx context.Context, request mcpRequest) *mcpResponse {
	if request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" {
		return ptrMCP(mcpErrorResponse(request.ID, -32600, "invalid JSON-RPC request"))
	}
	if len(request.ID) == 0 && strings.HasPrefix(request.Method, "notifications/") {
		return nil
	}
	switch request.Method {
	case "initialize":
		return mcpResult(request.ID, map[string]any{
			"protocolVersion": MCPProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "jangolova", "version": "0.2.0"},
		})
	case "notifications/initialized":
		return nil
	case "ping":
		return mcpResult(request.ID, map[string]any{})
	case "tools/list":
		return mcpResult(request.ID, map[string]any{"tools": jangolovaMCPTools()})
	case "tools/call":
		return server.dispatchToolCall(ctx, request)
	default:
		return ptrMCP(mcpErrorResponse(request.ID, -32601, "method not found"))
	}
}

func ptrMCP(value mcpResponse) *mcpResponse { return &value }
func mcpResult(id json.RawMessage, value any) *mcpResponse {
	return &mcpResponse{JSONRPC: "2.0", ID: id, Result: value}
}

func jangolovaMCPTools() []map[string]any {
	object := func(properties map[string]any, required ...string) map[string]any {
		value := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) != 0 {
			value["required"] = required
		}
		return value
	}
	return []map[string]any{
		{"name": "jangolova_engines", "description": "List available Jangolova target adapters and capabilities.", "inputSchema": object(nil)},
		{"name": "jangolova_instance_connect", "description": "Attach Jangolova to a caller-owned target.", "inputSchema": object(map[string]any{"request": map[string]any{"type": "object"}}, "request")},
		{"name": "jangolova_instance_describe", "description": "Inspect one attached interaction instance.", "inputSchema": object(map[string]any{"instanceId": map[string]any{"type": "string"}}, "instanceId")},
		{"name": "jangolova_instance_call", "description": "Call a negotiated semantic method on an attached instance.", "inputSchema": object(map[string]any{"instanceId": map[string]any{"type": "string"}, "method": map[string]any{"type": "string"}, "params": map[string]any{"type": "object"}, "approvalId": map[string]any{"type": "string"}}, "instanceId", "method")},
		{"name": "jangolova_instance_events", "description": "Read target and action audit events after an optional cursor.", "inputSchema": object(map[string]any{"instanceId": map[string]any{"type": "string"}, "after": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"}}, "instanceId")},
		{"name": "jangolova_action_approval_request", "description": "Create a pending owner approval for an action configured to require it.", "inputSchema": object(map[string]any{"instanceId": map[string]any{"type": "string"}, "params": map[string]any{"type": "object"}, "expiresInSeconds": map[string]any{"type": "integer"}}, "instanceId", "params")},
		{"name": "jangolova_action_approval_resolve", "description": "Approve or reject a pending action approval.", "inputSchema": object(map[string]any{"instanceId": map[string]any{"type": "string"}, "approvalId": map[string]any{"type": "string"}, "approved": map[string]any{"type": "boolean"}}, "instanceId", "approvalId", "approved")},
		{"name": "jangolova_instance_disconnect", "description": "Detach an interaction instance without stopping the caller-owned target.", "inputSchema": object(map[string]any{"instanceId": map[string]any{"type": "string"}}, "instanceId")},
	}
}

func (server *MCPServer) dispatchToolCall(ctx context.Context, request mcpRequest) *mcpResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil || strings.TrimSpace(params.Name) == "" {
		return ptrMCP(mcpErrorResponse(request.ID, -32602, "tools/call requires name and arguments"))
	}
	result, err := server.callTool(ctx, params.Name, params.Arguments)
	if err != nil {
		return mcpResult(request.ID, map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": err.Error()}}})
	}
	encoded, _ := json.Marshal(result)
	return mcpResult(request.ID, map[string]any{"isError": false, "content": []map[string]any{{"type": "text", "text": string(encoded)}}, "structuredContent": result})
}

func (server *MCPServer) callTool(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var args map[string]json.RawMessage
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, errors.New("tool arguments must be an object")
	}
	method, path := http.MethodGet, ""
	var body json.RawMessage
	switch name {
	case "jangolova_engines":
		path = "/v1/engines"
	case "jangolova_instance_connect":
		method, path, body = http.MethodPost, "/v1/instances", args["request"]
	case "jangolova_instance_describe":
		path = "/v1/instances/" + stringValue(args["instanceId"])
	case "jangolova_instance_call":
		method, path = http.MethodPost, "/v1/instances/"+stringValue(args["instanceId"])+"/call"
		body, _ = json.Marshal(map[string]json.RawMessage{"method": args["method"], "params": defaultJSON(args["params"], json.RawMessage(`{}`)), "approvalId": args["approvalId"]})
	case "jangolova_instance_events":
		path = "/v1/instances/" + stringValue(args["instanceId"]) + "/events"
		if after := stringValue(args["after"]); after != "" {
			path += "?after=" + after
		}
	case "jangolova_action_approval_request":
		method, path = http.MethodPost, "/v1/instances/"+stringValue(args["instanceId"])+"/approvals"
		body, _ = json.Marshal(map[string]json.RawMessage{"params": args["params"], "expiresInSeconds": args["expiresInSeconds"]})
	case "jangolova_action_approval_resolve":
		method, path = http.MethodPost, "/v1/instances/"+stringValue(args["instanceId"])+"/approvals/"+stringValue(args["approvalId"])
		body, _ = json.Marshal(map[string]json.RawMessage{"approved": args["approved"]})
	case "jangolova_instance_disconnect":
		method, path = http.MethodDelete, "/v1/instances/"+stringValue(args["instanceId"])
	default:
		return nil, fmt.Errorf("unknown Jangolova tool %q", name)
	}
	if strings.Contains(path, "//") || strings.HasSuffix(path, "/") {
		return nil, errors.New("instanceId is required")
	}
	request := httptest.NewRequest(method, path, strings.NewReader(string(body))).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+server.service.token)
	if len(body) != 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	server.service.Routes().ServeHTTP(recorder, request)
	if recorder.Code < 200 || recorder.Code >= 300 {
		var failure ErrorResponse
		_ = json.Unmarshal(recorder.Body.Bytes(), &failure)
		if failure.Message != "" {
			return nil, errors.New(failure.Message)
		}
		return nil, fmt.Errorf("Jangolova returned HTTP %d", recorder.Code)
	}
	if recorder.Code == http.StatusNoContent || recorder.Body.Len() == 0 {
		return map[string]any{"ok": true}, nil
	}
	var result any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		return nil, errors.New("Jangolova returned invalid JSON")
	}
	return result, nil
}

func defaultJSON(value, fallback json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return fallback
	}
	return value
}

func stringValue(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return strings.TrimSpace(value)
}

func newMCPSessionID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "mcp-local"
	}
	return "mcp-" + hex.EncodeToString(value)
}
