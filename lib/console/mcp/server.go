package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"console"
)

// Server publishes only a caller-supplied registry. It does not advertise
// sampling, resources, prompts, process management or background execution.
// ServeHTTP must be wrapped by the embedding host's authentication and origin
// policy. The HTTP transport is stateless, JSON POST with no SSE subscription.
type Server struct {
	Registry        *console.Registry
	ProtocolVersion string
	Name            string
	Version         string
	// PublicError controls the text exposed to remote callers. The default
	// contains no handler details, credentials or native runtime messages.
	PublicError func(error) string
}

func (s Server) Handle(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if s.Registry == nil || s.ProtocolVersion == "" || s.Name == "" {
		return nil, errors.New("MCP server needs a registry, protocol version and implementation name")
	}
	var request struct {
		Version string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &request) != nil || request.Version != "2.0" || request.Method == "" {
		return rpcError(json.RawMessage(`null`), -32600, "invalid request"), nil
	}
	if len(request.ID) == 0 {
		if request.Method != "notifications/initialized" && request.Method != "notifications/cancelled" {
			return nil, nil
		}
		return nil, nil
	}
	var id any
	if json.Unmarshal(request.ID, &id) != nil {
		return rpcError(json.RawMessage(`null`), -32600, "invalid request ID"), nil
	}
	switch id.(type) {
	case string, float64:
	default:
		return rpcError(json.RawMessage(`null`), -32600, "invalid request ID"), nil
	}
	var result any
	switch request.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(request.Params, &params) != nil || params.ProtocolVersion != s.ProtocolVersion {
			return rpcError(request.ID, -32602, "unsupported protocol version"), nil
		}
		result = map[string]any{"protocolVersion": s.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": s.Name, "version": s.Version}}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		var params struct {
			Cursor string `json:"cursor"`
		}
		if len(request.Params) > 0 && string(request.Params) != "null" {
			if json.Unmarshal(request.Params, &params) != nil || params.Cursor != "" {
				return rpcError(request.ID, -32602, "catalog has no continuation cursor"), nil
			}
		}
		result = map[string]any{"tools": s.Registry.Definitions()}
	case "tools/call":
		var call console.Call
		if json.Unmarshal(request.Params, &call) != nil {
			return rpcError(request.ID, -32602, "invalid tool arguments"), nil
		}
		output, err := s.Registry.Invoke(ctx, call)
		if err != nil {
			if errors.Is(err, console.ErrToolNotFound) || errors.Is(err, console.ErrInvalidCall) {
				return rpcError(request.ID, -32602, "unknown tool or invalid arguments"), nil
			}
			message := "tool invocation failed"
			if s.PublicError != nil {
				message = s.PublicError(err)
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": message}}, "isError": true}
		} else {
			answer := map[string]any{"content": []any{map[string]any{"type": "text", "text": string(output.Output)}}, "isError": output.IsError}
			var object map[string]any
			if json.Unmarshal(output.Output, &object) == nil && object != nil {
				answer["structuredContent"] = object
			}
			result = answer
		}
	default:
		return rpcError(request.ID, -32601, "method not found"), nil
	}
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return rpcError(request.ID, -32603, "result exceeds byte ceiling"), nil
	}
	return data, nil
}
func rpcError(id json.RawMessage, code int, message string) json.RawMessage {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": RPCError{Code: code, Message: message}})
	return data
}

func (s Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST is required", http.StatusMethodNotAllowed)
		return
	}
	if version := r.Header.Get("MCP-Protocol-Version"); version != "" && version != s.ProtocolVersion {
		http.Error(w, "unsupported protocol version", http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "request exceeds byte ceiling", http.StatusRequestEntityTooLarge)
		return
	}
	answer, err := s.Handle(r.Context(), data)
	if err != nil {
		http.Error(w, "MCP server unavailable", http.StatusServiceUnavailable)
		return
	}
	if len(answer) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(answer)
}

// ServeIO borrows supplied JSON-lines streams. It never spawns a process or
// closes them; the host must unblock its reader when canceling the attachment.
func (s Server) ServeIO(ctx context.Context, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20+1)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		response, err := s.Handle(ctx, scanner.Bytes())
		if err != nil {
			return err
		}
		if len(response) > 0 {
			if _, err = fmt.Fprintf(output, "%s\n", response); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return ctx.Err()
}
