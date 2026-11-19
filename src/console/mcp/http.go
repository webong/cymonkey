package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// HTTPTransport implements Streamable HTTP with JSON or SSE POST responses.
// Notifications and server requests are not executed. The host owns the server.
type HTTPTransport struct {
	mu               sync.Mutex
	endpoint         string
	client           *http.Client
	headers          http.Header
	session, version string
	nextID           uint64
}

func NewHTTP(endpoint string, client *http.Client, headers http.Header) (*HTTPTransport, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("MCP endpoint must be an HTTP(S) URL without user credentials")
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &HTTPTransport{endpoint: endpoint, client: client, headers: headers.Clone()}, nil
}
func (t *HTTPTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	id := t.nextID
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	resp, err := t.send(ctx, http.MethodPost, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("MCP endpoint returned HTTP %d", resp.StatusCode)
	}
	var raw []byte
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	if mediaType == "text/event-stream" {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, 16<<20+1))
		scanner.Buffer(make([]byte, 4096), 16<<20+1)
		var lines []string
		var total int
		flush := func() error {
			if len(lines) == 0 {
				return nil
			}
			candidate := []byte(strings.Join(lines, "\n"))
			lines = nil
			var reply rpcReply
			if json.Unmarshal(candidate, &reply) != nil {
				return errors.New("invalid MCP SSE event")
			}
			if len(reply.ID) == 0 || reply.Method != "" {
				return nil
			}
			expected, _ := json.Marshal(id)
			if string(reply.ID) == string(expected) {
				raw = candidate
			}
			return nil
		}
		for scanner.Scan() {
			line := scanner.Text()
			total += len(line) + 1
			if total > 16<<20 {
				return nil, errors.New("MCP response exceeds 16 MiB")
			}
			if line == "" && len(lines) > 0 {
				if err := flush(); err != nil {
					return nil, err
				}
				if len(raw) > 0 {
					break
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		if len(raw) == 0 {
			return nil, io.ErrUnexpectedEOF
		}
	} else if mediaType == "application/json" {
		raw, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20+1))
		if err != nil {
			return nil, err
		}
		if len(raw) > 16<<20 {
			return nil, errors.New("MCP response exceeds 16 MiB")
		}
	} else {
		return nil, errors.New("MCP endpoint returned unsupported content type")
	}
	result, err := replyResult(raw, id)
	if err != nil {
		return nil, err
	}
	if method == "initialize" {
		t.session = resp.Header.Get("Mcp-Session-Id")
		var init struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err = json.Unmarshal(result, &init); err != nil {
			return nil, err
		}
		t.version = init.ProtocolVersion
	}
	return result, nil
}
func (t *HTTPTransport) Notify(ctx context.Context, method string, params any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	payload := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		payload["params"] = params
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := t.send(ctx, http.MethodPost, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("MCP notification returned HTTP %d", resp.StatusCode)
	}
	return nil
}
func (t *HTTPTransport) send(ctx context.Context, method string, data []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, t.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header = t.headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if t.session != "" {
		req.Header.Set("Mcp-Session-Id", t.session)
	}
	if t.version != "" {
		req.Header.Set("MCP-Protocol-Version", t.version)
	}
	return t.client.Do(req)
}

// Close releases only this MCP session, never the server process.
func (t *HTTPTransport) Close(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.session == "" {
		return nil
	}
	resp, err := t.send(ctx, http.MethodDelete, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusMethodNotAllowed {
		return fmt.Errorf("MCP session close returned HTTP %d", resp.StatusCode)
	}
	t.session = ""
	return nil
}
