package engineprovider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cymonkey/internal/orchestrator"
)

func TestMCPServerExposesDirectEngineTools(t *testing.T) {
	registry := orchestrator.NewRegistry()
	if err := registry.RegisterEngine("fake", &fakeEngineAdapter{capabilities: []string{"describe", "act"}}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := NewMCPServer(service)
	if err != nil {
		t.Fatal(err)
	}

	recorder := performAuthorizedMCP(mcp, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, "")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Mcp-Session-Id") == "" {
		t.Fatalf("initialize status = %d headers=%#v", recorder.Code, recorder.Header())
	}
	sessionID := recorder.Header().Get("Mcp-Session-Id")
	recorder = performAuthorizedMCP(mcp, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, sessionID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	foundCall, foundObserve := false, false
	for _, tool := range response.Result.Tools {
		if tool.Name == "jangolova_instance_call" {
			foundCall = true
		}
		if tool.Name == "jangolova_instance_observe" {
			foundObserve = true
		}
	}
	if !foundCall || !foundObserve {
		t.Fatalf("MCP tools = %#v", response.Result.Tools)
	}
}

func performAuthorizedMCP(server *MCPServer, body, sessionID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, request)
	return recorder
}
