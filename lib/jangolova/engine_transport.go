package jangolova

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"jangolova/sdk"
)

const engineMaxMessageBytes = 4 * 1024 * 1024

// EngineTransport carries the transport-neutral request/response methods of
// the Cymonkey render domain.
type EngineTransport interface {
	sdk.Caller
	Close() error
}

// EngineConnector binds the render domain to one target-descriptor endpoint
// protocol.
type EngineConnector interface {
	Protocol() string
	Connect(context.Context, sdk.TargetEndpoint) (EngineTransport, error)
}

// EngineWebSocketConnector is the authenticated WebSocket transport binding
// for render-domain peers.
type EngineWebSocketConnector struct{ Host sdk.Host }

func (EngineWebSocketConnector) Protocol() string { return "websocket" }

func (c EngineWebSocketConnector) Connect(ctx context.Context, endpoint sdk.TargetEndpoint) (EngineTransport, error) {
	parsed, err := url.Parse(endpoint.URL)
	if err != nil || parsed.Scheme != "ws" && parsed.Scheme != "wss" || parsed.User != nil {
		return nil, errors.New("Cymonkey WebSocket endpoint must be an absolute ws or wss URL without user information")
	}
	if c.Host.DialWebSocket == nil {
		return nil, errors.New("Jangolova host WebSocket dialer is required")
	}
	connection, err := c.Host.DialWebSocket(ctx, endpoint)
	if err != nil {
		return nil, err
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
	Type   string          `json:"type,omitempty"`
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
	cancelled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = t.connection.SetReadDeadline(time.Now())
		_ = t.connection.SetWriteDeadline(time.Now())
		close(cancelled)
	})
	defer func() {
		if !stop() {
			<-cancelled
		}
		_ = t.connection.SetReadDeadline(time.Time{})
		_ = t.connection.SetWriteDeadline(time.Time{})
	}()
	t.nextID++
	if err := t.connection.WriteJSON(engineRequest{ID: t.nextID, Method: method, Params: params}); err != nil {
		return nil, errors.New("write Cymonkey WebSocket request")
	}
	var reply engineResponse
	for notices := 0; ; notices++ {
		if notices > 4 {
			return nil, errors.New("too many WebSocket control notices")
		}
		reply = engineResponse{}
		if err := t.connection.ReadJSON(&reply); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("read Jangolova WebSocket response")
		}
		if reply.Type == "cymonkey.authenticated" && reply.ID == 0 {
			continue
		}
		if reply.Type == "cymonkey.authorization_required" {
			return nil, errors.New("WebSocket authentication was rejected")
		}
		break
	}
	if reply.ID != t.nextID {
		return nil, errors.New("Cymonkey response id does not match request")
	}
	if reply.Error != nil {
		return nil, &sdk.RemoteError{Code: strings.TrimSpace(reply.Error.Code), Message: reply.Error.Message}
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
