package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketHostAuthenticatesAndCalls(t *testing.T) {
	t.Parallel()

	host, err := NewWebSocketHost("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewWebSocketHost() error = %v", err)
	}
	t.Cleanup(func() {
		if err := host.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	if _, response, err := websocket.DefaultDialer.Dial(
		host.Endpoint(),
		nil,
	); err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		if response != nil {
			response.Body.Close()
		}
		t.Fatalf("unauthenticated Dial() response=%v error=%v", response, err)
	} else {
		response.Body.Close()
	}

	headers := http.Header{
		"Authorization": []string{"Bearer " + host.Token()},
	}
	engine, _, err := websocket.DefaultDialer.Dial(host.Endpoint(), headers)
	if err != nil {
		t.Fatalf("authenticated Dial() error = %v", err)
	}
	defer engine.Close()
	go serveFixtureBridge(t, engine)

	if second, response, err := websocket.DefaultDialer.Dial(
		host.Endpoint(),
		headers,
	); err == nil ||
		response == nil ||
		response.StatusCode != http.StatusConflict {
		if second != nil {
			second.Close()
		}
		if response != nil {
			response.Body.Close()
		}
		t.Fatalf("second Dial() response=%v error=%v", response, err)
	} else {
		response.Body.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, err := host.WaitConnection(ctx)
	if err != nil {
		t.Fatalf("WaitConnection() error = %v", err)
	}
	result, err := connection.Call(
		ctx,
		MethodDescribe,
		json.RawMessage(`{"detail":"summary"}`),
	)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if string(result) != `{"ready":true}` {
		t.Fatalf("Call() result = %s", result)
	}
}

func TestWebSocketHostRejectsNonLoopbackAddress(t *testing.T) {
	t.Parallel()

	_, err := NewWebSocketHost("0.0.0.0:0")
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("NewWebSocketHost() error = %v", err)
	}
}

func serveFixtureBridge(t *testing.T, connection *websocket.Conn) {
	t.Helper()
	for {
		var request webSocketRequest
		if err := connection.ReadJSON(&request); err != nil {
			return
		}
		result := json.RawMessage(`{"ready":true}`)
		if err := connection.WriteJSON(webSocketResponse{
			ID:     request.ID,
			Result: result,
		}); err != nil {
			return
		}
	}
}
