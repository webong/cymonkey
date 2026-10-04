package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
)

func (c *Client) requireCapability(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ready {
		return errors.New("MCP client is not initialized")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(c.capabilities[name], &object) != nil || object == nil {
		return errors.New("MCP server does not advertise requested capability")
	}
	return nil
}

// Context catalogs and contents stay as native JSON so annotations, embedded
// resources and future additive metadata are preserved. None is inserted into
// a model transcript or treated as executable instructions by this library.
func (c *Client) contextList(ctx context.Context, capability, method, key string) ([]json.RawMessage, error) {
	if err := c.requireCapability(capability); err != nil {
		return nil, err
	}
	result := []json.RawMessage{}
	cursor := ""
	seen := map[string]bool{}
	var total int
	for page := 0; page < 128; page++ {
		params := map[string]string{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.transport.Call(ctx, method, params)
		if err != nil {
			return nil, err
		}
		total += len(raw)
		if total > 16<<20 {
			return nil, errors.New("MCP catalog exceeds byte ceiling")
		}
		var envelope map[string]json.RawMessage
		if json.Unmarshal(raw, &envelope) != nil {
			return nil, errors.New("invalid MCP catalog")
		}
		var entries []json.RawMessage
		if json.Unmarshal(envelope[key], &entries) != nil || entries == nil || len(result)+len(entries) > 4096 {
			return nil, errors.New("invalid or oversized MCP catalog")
		}
		for _, entry := range entries {
			var object map[string]json.RawMessage
			if json.Unmarshal(entry, &object) != nil || object == nil {
				return nil, errors.New("MCP catalog entry must be an object")
			}
		}
		result = append(result, entries...)
		cursor = ""
		if next := envelope["nextCursor"]; len(next) > 0 && string(next) != "null" {
			if json.Unmarshal(next, &cursor) != nil || len(cursor) > 8192 {
				return nil, errors.New("invalid MCP cursor")
			}
		}
		if cursor == "" {
			return result, nil
		}
		if seen[cursor] {
			return nil, errors.New("MCP pagination cursor repeated")
		}
		seen[cursor] = true
	}
	return nil, errors.New("MCP pagination exceeds ceiling")
}

func (c *Client) Resources(ctx context.Context) ([]json.RawMessage, error) {
	return c.contextList(ctx, "resources", "resources/list", "resources")
}
func (c *Client) ResourceTemplates(ctx context.Context) ([]json.RawMessage, error) {
	return c.contextList(ctx, "resources", "resources/templates/list", "resourceTemplates")
}
func (c *Client) Prompts(ctx context.Context) ([]json.RawMessage, error) {
	return c.contextList(ctx, "prompts", "prompts/list", "prompts")
}

// ReadResource asks only the selected server for an explicit URI. It never
// follows file/http links, opens local paths or subscribes to resource updates.
func (c *Client) ReadResource(ctx context.Context, uri string) (json.RawMessage, error) {
	if err := c.requireCapability("resources"); err != nil {
		return nil, err
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme == "" || len(uri) > 8192 {
		return nil, errors.New("invalid MCP resource URI")
	}
	raw, err := c.transport.Call(ctx, "resources/read", map[string]string{"uri": uri})
	if err != nil {
		return nil, err
	}
	if len(raw) > 16<<20 {
		return nil, errors.New("MCP resource exceeds byte ceiling")
	}
	var result struct {
		Contents []struct {
			URI  string  `json:"uri"`
			Text *string `json:"text"`
			Blob *string `json:"blob"`
		} `json:"contents"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Contents == nil || len(result.Contents) > 1024 {
		return nil, errors.New("invalid MCP resource content")
	}
	for _, content := range result.Contents {
		if content.URI == "" || (content.Text == nil) == (content.Blob == nil) {
			return nil, errors.New("invalid MCP resource representation")
		}
		if content.Blob != nil {
			if _, err := base64.StdEncoding.DecodeString(*content.Blob); err != nil {
				return nil, errors.New("invalid MCP binary resource")
			}
		}
	}
	return raw, nil
}

// GetPrompt returns inert native message data. The host decides whether to
// display it or copy selected messages into its own model request.
func (c *Client) GetPrompt(ctx context.Context, name string, arguments map[string]string) (json.RawMessage, error) {
	if err := c.requireCapability("prompts"); err != nil {
		return nil, err
	}
	if name == "" || len(name) > 256 || len(arguments) > 128 {
		return nil, errors.New("invalid MCP prompt selection")
	}
	params := map[string]any{"name": name, "arguments": arguments}
	encoded, err := json.Marshal(params)
	if err != nil || len(encoded) > 1<<20 {
		return nil, errors.New("MCP prompt arguments exceed ceiling")
	}
	raw, err := c.transport.Call(ctx, "prompts/get", params)
	if err != nil {
		return nil, err
	}
	if len(raw) > 16<<20 {
		return nil, errors.New("MCP prompt exceeds byte ceiling")
	}
	var result struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Messages == nil || len(result.Messages) > 1024 {
		return nil, errors.New("invalid MCP prompt result")
	}
	for _, message := range result.Messages {
		var content map[string]json.RawMessage
		if message.Role != "user" && message.Role != "assistant" || json.Unmarshal(message.Content, &content) != nil || content == nil {
			return nil, errors.New("invalid MCP prompt message")
		}
	}
	return raw, nil
}
