package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	webSocketPath      = "/bridge"
	maxBridgeMessage   = 4 * 1024 * 1024
	defaultHTTPTimeout = 5 * time.Second
)

// WebSocketHostProvider is implemented by interaction engines that accept a
// cooperative connection from a caller-owned native target.
type WebSocketHostProvider interface {
	BridgeWebSocketHost() *WebSocketHost
}

// WebSocketHost accepts one authenticated outbound connection from an engine.
type WebSocketHost struct {
	mu         sync.Mutex
	listener   net.Listener
	server     *http.Server
	endpoint   string
	token      string
	connection *WebSocketConnection
	connecting bool
	ready      chan struct{}
	closed     chan struct{}
	closeOnce  sync.Once
}

// NewWebSocketHost binds a loopback address and starts the bridge HTTP server.
// A port of zero selects an ephemeral port.
func NewWebSocketHost(address string) (*WebSocketHost, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		address = "127.0.0.1:0"
	}
	if err := validateLoopbackAddress(address); err != nil {
		return nil, err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate bridge token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen for native bridge: %w", err)
	}
	host := &WebSocketHost{
		listener: listener,
		token:    token,
		ready:    make(chan struct{}),
		closed:   make(chan struct{}),
	}
	endpoint := url.URL{
		Scheme: "ws",
		Host:   listener.Addr().String(),
		Path:   webSocketPath,
	}
	host.endpoint = endpoint.String()
	mux := http.NewServeMux()
	mux.HandleFunc(webSocketPath, host.accept)
	host.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: defaultHTTPTimeout,
		IdleTimeout:       defaultHTTPTimeout,
	}
	go func() {
		_ = host.server.Serve(listener)
		host.markClosed()
	}()
	return host, nil
}

func validateLoopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse native bridge address: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf(
			"native bridge address must use a loopback host, got %q",
			host,
		)
	}
	return nil
}

func (h *WebSocketHost) Endpoint() string {
	return h.endpoint
}

// Token is a short-lived interaction credential. A target owner such as Xallet
// injects it into the caller-owned native process; it must not be logged or
// placed in the endpoint URL.
func (h *WebSocketHost) Token() string {
	return h.token
}

func (h *WebSocketHost) accept(writer http.ResponseWriter, request *http.Request) {
	if !h.authorized(request.Header.Get("Authorization")) {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	if request.Header.Get("Origin") != "" {
		http.Error(writer, "browser origins are not accepted", http.StatusForbidden)
		return
	}

	h.mu.Lock()
	if h.connection != nil || h.connecting {
		h.mu.Unlock()
		http.Error(writer, "bridge is already connected", http.StatusConflict)
		return
	}
	h.connecting = true
	h.mu.Unlock()

	upgrader := websocket.Upgrader{
		HandshakeTimeout: defaultHTTPTimeout,
		CheckOrigin: func(*http.Request) bool {
			return true
		},
	}
	connection, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		h.mu.Lock()
		h.connecting = false
		h.mu.Unlock()
		return
	}
	connection.SetReadLimit(maxBridgeMessage)

	h.mu.Lock()
	h.connecting = false
	h.connection = &WebSocketConnection{connection: connection}
	close(h.ready)
	h.mu.Unlock()
}

func (h *WebSocketHost) authorized(header string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	supplied := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if len(supplied) != len(h.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(h.token)) == 1
}

func (h *WebSocketHost) WaitConnection(
	ctx context.Context,
) (*WebSocketConnection, error) {
	select {
	case <-h.ready:
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.connection == nil {
			return nil, errors.New("native bridge closed before connection became ready")
		}
		return h.connection, nil
	case <-h.closed:
		return nil, errors.New("native bridge host is closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *WebSocketHost) Close(ctx context.Context) error {
	h.markClosed()
	h.mu.Lock()
	connection := h.connection
	h.connection = nil
	h.mu.Unlock()

	var problems []error
	if connection != nil {
		if err := connection.Close(); err != nil {
			problems = append(problems, err)
		}
	}
	if h.server != nil {
		if err := h.server.Shutdown(ctx); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

func (h *WebSocketHost) markClosed() {
	h.closeOnce.Do(func() {
		close(h.closed)
	})
}

// WebSocketConnection implements Caller over the native bridge envelope.
type WebSocketConnection struct {
	mu         sync.Mutex
	connection *websocket.Conn
	nextID     uint64
	closed     bool
}

type webSocketRequest struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type webSocketResponse struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    string `json:"code,omitempty"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *WebSocketConnection) Call(
	ctx context.Context,
	method string,
	params json.RawMessage,
) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.connection == nil {
		return nil, errors.New("native bridge connection is closed")
	}
	if strings.TrimSpace(method) == "" {
		return nil, errors.New("native bridge method is required")
	}
	if len(bytes.TrimSpace(params)) == 0 {
		params = json.RawMessage(`{}`)
	}
	if !json.Valid(params) {
		return nil, errors.New("native bridge params are invalid JSON")
	}
	if err := applyWebSocketDeadline(ctx, c.connection); err != nil {
		return nil, err
	}
	defer func() {
		_ = c.connection.SetReadDeadline(time.Time{})
		_ = c.connection.SetWriteDeadline(time.Time{})
	}()

	c.nextID++
	request := webSocketRequest{
		ID:     c.nextID,
		Method: method,
		Params: params,
	}
	if err := c.connection.WriteJSON(request); err != nil {
		return nil, fmt.Errorf("write native bridge request: %w", err)
	}
	_, payload, err := c.connection.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read native bridge response: %w", err)
	}
	var response webSocketResponse
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode native bridge response: %w", err)
	}
	if response.ID != request.ID {
		return nil, fmt.Errorf(
			"native bridge response id %d does not match request %d",
			response.ID,
			request.ID,
		)
	}
	if response.Error != nil {
		message := strings.TrimSpace(response.Error.Message)
		if message == "" {
			message = "native bridge returned an error"
		}
		if response.Error.Code != "" {
			message = response.Error.Code + ": " + message
		}
		return nil, errors.New(message)
	}
	if !validJSONValue(response.Result) {
		return nil, errors.New("native bridge response result is invalid JSON")
	}
	return response.Result, nil
}

func applyWebSocketDeadline(
	ctx context.Context,
	connection *websocket.Conn,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetReadDeadline(deadline); err != nil {
			return err
		}
		return connection.SetWriteDeadline(deadline)
	}
	return nil
}

func (c *WebSocketConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.connection == nil {
		return nil
	}
	c.closed = true
	message := websocket.FormatCloseMessage(
		websocket.CloseNormalClosure,
		"interaction disconnecting",
	)
	_ = c.connection.WriteControl(
		websocket.CloseMessage,
		message,
		time.Now().Add(time.Second),
	)
	return c.connection.Close()
}
