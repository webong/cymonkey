// Package mcp adapts tools from explicitly supplied MCP connections into
// Console tools. It does not discover servers, launch processes, or execute
// tools during initialization/discovery.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"console"
)

type Transport interface {
	Call(context.Context, string, any) (json.RawMessage, error)
	Notify(context.Context, string, any) error
}

type Client struct {
	mu           sync.Mutex
	transport    Transport
	ready        bool
	capabilities map[string]json.RawMessage
}

func New(transport Transport) (*Client, error) {
	if transport == nil {
		return nil, errors.New("MCP transport is required")
	}
	return &Client{transport: transport}, nil
}

// Initialize negotiates the protocol version selected by the host. A version
// change is reported instead of silently accepting an unsupported protocol.
func (c *Client) Initialize(ctx context.Context, version string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ready {
		return errors.New("MCP client is already initialized")
	}
	if version == "" {
		return errors.New("MCP protocol version is required")
	}
	raw, err := c.transport.Call(ctx, "initialize", map[string]any{"protocolVersion": version, "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "console", "version": "0.1.0"}})
	if err != nil {
		return err
	}
	var result struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if result.ProtocolVersion != version {
		return fmt.Errorf("MCP server negotiated unsupported version %q", result.ProtocolVersion)
	}
	if result.Capabilities == nil {
		return errors.New("MCP server returned invalid capabilities")
	}
	if err = c.transport.Notify(ctx, "notifications/initialized", nil); err != nil {
		return err
	}
	c.ready = true
	c.capabilities = result.Capabilities
	return nil
}

type toolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Tools returns a snapshot of discovered tools. prefix is a host-chosen
// namespace. The host selects and registers the returned tools itself.
func (c *Client) Tools(ctx context.Context, prefix string) ([]console.Tool, error) {
	if err := c.requireCapability("tools"); err != nil {
		return nil, err
	}
	var tools []console.Tool
	cursor := ""
	cursors := map[string]bool{}
	names := map[string]bool{}
	var catalogBytes int
	for page := 0; page < 128; page++ {
		params := map[string]string{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.transport.Call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		catalogBytes += len(raw)
		if catalogBytes > 16<<20 {
			return nil, errors.New("MCP tool catalog exceeds byte ceiling")
		}
		var result struct {
			Tools      []toolSpec `json:"tools"`
			NextCursor string     `json:"nextCursor"`
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			return nil, err
		}
		if result.Tools == nil || len(tools)+len(result.Tools) > 4096 {
			return nil, errors.New("MCP tool catalog is invalid or too large")
		}
		for _, spec := range result.Tools {
			if spec.Name == "" || names[spec.Name] {
				return nil, errors.New("MCP tool catalog contains invalid or duplicate names")
			}
			names[spec.Name] = true
			tool, err := console.NewResultTool(console.Definition{Name: prefix + spec.Name, Description: spec.Description, InputSchema: spec.InputSchema}, func(ctx context.Context, args json.RawMessage) (console.Result, error) {
				data, err := c.transport.Call(ctx, "tools/call", map[string]any{"name": spec.Name, "arguments": args})
				if err != nil {
					return console.Result{}, err
				}
				var envelope struct {
					Content []json.RawMessage `json:"content"`
					IsError bool              `json:"isError"`
				}
				if json.Unmarshal(data, &envelope) != nil || envelope.Content == nil {
					return console.Result{}, errors.New("invalid MCP tool result")
				}
				return console.Result{Output: data, IsError: envelope.IsError}, nil
			})
			if err != nil {
				return nil, err
			}
			tools = append(tools, tool)
		}
		cursor = result.NextCursor
		if cursor == "" {
			return tools, nil
		}
		if cursors[cursor] {
			return nil, errors.New("MCP pagination cursor repeated")
		}
		cursors[cursor] = true
	}
	return nil, errors.New("MCP tool catalog exceeds pagination limit")
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message) }

type rpcReply struct {
	Version string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

func replyResult(raw []byte, id uint64) (json.RawMessage, error) {
	var reply rpcReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, err
	}
	expected, _ := json.Marshal(id)
	if reply.Version != "2.0" || string(reply.ID) != string(expected) {
		return nil, errors.New("MCP reply does not match request")
	}
	if reply.Error != nil {
		if len(reply.Result) > 0 {
			return nil, errors.New("MCP reply contains both result and error")
		}
		return nil, reply.Error
	}
	if !json.Valid(reply.Result) {
		return nil, errors.New("MCP reply has no result")
	}
	return reply.Result, nil
}
