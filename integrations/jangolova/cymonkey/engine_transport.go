package cymonkey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"jangolova/internal/bridge"
	"jangolova/internal/orchestrator"
	"jangolova/targetconn"
)

const engineMaxMessageBytes = 4 * 1024 * 1024

// EngineTransport carries the transport-neutral request/response methods of
// the Cymonkey render domain.
type EngineTransport interface {
	bridge.Caller
	Close() error
}

// EngineConnector binds the render domain to one target-descriptor endpoint
// protocol.
type EngineConnector interface {
	Protocol() string
	Connect(context.Context, orchestrator.TargetEndpoint) (EngineTransport, error)
}

// EngineWebSocketConnector is the authenticated cymonkey-ws binding for
// render-domain peers.
type EngineWebSocketConnector struct{}

func (EngineWebSocketConnector) Protocol() string { return "cymonkey-ws" }

func (EngineWebSocketConnector) Connect(ctx context.Context, endpoint orchestrator.TargetEndpoint) (EngineTransport, error) {
	parsed, err := url.Parse(endpoint.URL)
	if err != nil || parsed.Scheme != "ws" && parsed.Scheme != "wss" || parsed.User != nil {
		return nil, errors.New("Cymonkey WebSocket endpoint must be an absolute ws or wss URL without user information")
	}
	dialer, connectionHeaders, err := targetconn.WebSocketDialer(endpoint)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	for name, value := range connectionHeaders {
		headers.Set(name, value)
	}
	connection, response, err := dialer.DialContext(ctx, endpoint.URL, headers)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, errors.New("connect to caller-owned Cymonkey WebSocket endpoint")
	}
	connection.SetReadLimit(engineMaxMessageBytes)
	return &engineWebSocketTransport{connection: connection}, nil
}

type engineWebSocketTransport struct {
	mu         sync.Mutex
	connection *websocket.Conn
	nextID     uint64
	closed     bool
}

type engineRequest struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type engineResponse struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (t *engineWebSocketTransport) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.connection == nil {
		return nil, errors.New("Cymonkey WebSocket transport is closed")
	}
	if len(bytes.TrimSpace(params)) == 0 {
		params = json.RawMessage(`{}`)
	}
	if !json.Valid(params) {
		return nil, errors.New("Cymonkey params are invalid JSON")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = t.connection.SetWriteDeadline(deadline)
		_ = t.connection.SetReadDeadline(deadline)
		defer t.connection.SetWriteDeadline(time.Time{})
		defer t.connection.SetReadDeadline(time.Time{})
	}
	t.nextID++
	if err := t.connection.WriteJSON(engineRequest{ID: t.nextID, Method: method, Params: params}); err != nil {
		return nil, errors.New("write Cymonkey WebSocket request")
	}
	var reply engineResponse
	if err := t.connection.ReadJSON(&reply); err != nil {
		return nil, errors.New("read Cymonkey WebSocket response")
	}
	if reply.ID != t.nextID {
		return nil, errors.New("Cymonkey response id does not match request")
	}
	if reply.Error != nil {
		return nil, fmt.Errorf("cymonkey %s: %s", strings.TrimSpace(reply.Error.Code), reply.Error.Message)
	}
	if !json.Valid(reply.Result) {
		return nil, errors.New("Cymonkey response is invalid JSON")
	}
	return reply.Result, nil
}

func (t *engineWebSocketTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	if t.connection == nil {
		return nil
	}
	_ = t.connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Jangolova detached"), time.Now().Add(time.Second))
	err := t.connection.Close()
	t.connection = nil
	return err
}
