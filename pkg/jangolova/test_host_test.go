package jangolova

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"

	"jangolova/host"
	"jangolova/sdk"
)

func testHost() sdk.Host {
	return host.Services(host.Dependencies{
		ResolveWorker: func(kind string) (string, error) {
			if kind != "browser" {
				return "", errors.New("test worker kind is unsupported")
			}
			return filepath.Abs("../../tests/cymonkey-worker-fixture.mjs")
		},
		ValidateEndpoint: func(endpoint sdk.TargetEndpoint) error {
			parsed, err := url.Parse(endpoint.URL)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				return errors.New("test endpoint must be an absolute URL")
			}
			switch parsed.Scheme {
			case "http", "https", "ws", "wss":
				return nil
			default:
				return errors.New("test endpoint scheme is not supported")
			}
		},
		WorkerEnvironment: func(_ sdk.TargetEndpoint, environment []string) ([]string, error) {
			return append(os.Environ(), environment...), nil
		},
		StartWorker: func(string, string, []string, []string) (sdk.Worker, error) {
			return newFixtureWorker(), nil
		},
		DialWebSocket: func(ctx context.Context, endpoint sdk.TargetEndpoint) (*websocket.Conn, error) {
			conn, _, err := websocket.DefaultDialer.DialContext(ctx, endpoint.URL, nil)
			return conn, err
		},
		ListenWebSocket: func(string) (sdk.Listener, error) {
			return newTestListener()
		},
		ConnectSafari: func(context.Context, sdk.EngineSpec, sdk.EngineTarget) (sdk.EngineInstance, error) {
			return nil, errors.New("test host does not provide Safari")
		},
		Redact: func(message string, _ sdk.EngineTarget) string { return message },
	})
}

type fixtureWorker struct {
	done chan struct{}
	once sync.Once
}

func newFixtureWorker() *fixtureWorker {
	return &fixtureWorker{done: make(chan struct{})}
}

func (w *fixtureWorker) Call(ctx context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	switch method {
	case "connect":
		return json.RawMessage(`{"capabilities":["script.register"]}`), nil
	case "reconnect":
		return json.RawMessage(`{"reconnected":true}`), nil
	case "health":
		return json.RawMessage(`{"connected":true}`), nil
	case "capabilities":
		return json.RawMessage(`["script.register"]`), nil
	case "events":
		return json.RawMessage(`{"events":[],"cursor":"0"}`), nil
	case "disconnect":
		w.finish()
		return json.RawMessage(`{"disconnected":true}`), nil
	default:
		return json.RawMessage(`{}`), nil
	}
}

func (w *fixtureWorker) Disconnect(context.Context) error {
	w.finish()
	return nil
}

func (w *fixtureWorker) Terminate()            { w.finish() }
func (w *fixtureWorker) Done() <-chan struct{} { return w.done }
func (*fixtureWorker) WaitError() error        { return nil }
func (*fixtureWorker) StderrSuffix() string    { return "" }

func (w *fixtureWorker) finish() {
	w.once.Do(func() { close(w.done) })
}

type testListener struct {
	server *httptest.Server
	token  string
	conns  chan *websocket.Conn
	once   sync.Once
}

func newTestListener() (sdk.Listener, error) {
	listener := &testListener{token: "fixture-listener-token", conns: make(chan *websocket.Conn, 1)}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	listener.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+listener.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		select {
		case listener.conns <- conn:
		case <-r.Context().Done():
			_ = conn.Close()
		}
	}))
	return listener, nil
}

func (l *testListener) Endpoint() string { return "ws" + strings.TrimPrefix(l.server.URL, "http") }
func (l *testListener) Token() string    { return l.token }
func (l *testListener) WaitConnection(ctx context.Context) (sdk.Transport, error) {
	select {
	case conn := <-l.conns:
		return &testTransport{conn: conn}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (l *testListener) Close(context.Context) error {
	l.once.Do(func() { l.server.Close() })
	return nil
}

type testTransport struct {
	conn *websocket.Conn
	next atomic.Uint64
}

func (t *testTransport) Call(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	id := t.next.Add(1)
	if err := t.conn.WriteJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		var response struct {
			ID     uint64          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if err := t.conn.ReadJSON(&response); err != nil {
			return nil, err
		}
		if response.ID != id {
			continue
		}
		if response.Error != "" {
			return nil, errors.New(response.Error)
		}
		return response.Result, nil
	}
}

func (t *testTransport) Close() error { return t.conn.Close() }
