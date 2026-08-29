//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

const maxControlMessageBytes = 4 * 1024 * 1024

type controlEndpoint struct {
	url   *url.URL
	token string
}

func newControlEndpoint(rawURL, token string) (controlEndpoint, error) {
	endpoint, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || endpoint == nil || endpoint.User != nil || endpoint.Host == "" || (endpoint.Scheme != "ws" && endpoint.Scheme != "wss") {
		return controlEndpoint{}, errors.New("invalid authenticated Cymonkey control endpoint")
	}
	if strings.TrimSpace(token) == "" {
		return controlEndpoint{}, errors.New("missing Cymonkey control token")
	}
	if endpoint.Scheme == "ws" && !loopbackHost(endpoint.Hostname()) {
		return controlEndpoint{}, errors.New("plaintext Cymonkey control endpoint must use loopback")
	}
	return controlEndpoint{url: endpoint, token: token}, nil
}

func loopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

type controlRequest struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type controlError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type controlResponse struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *controlError   `json:"error,omitempty"`
}

func (endpoint controlEndpoint) run(runtime *windowsRuntime) error {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+endpoint.token)
	headers.Set("X-Jangolova-Protocol", protocolVersion)
	connection, response, err := websocket.DefaultDialer.Dial(endpoint.url.String(), headers)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return errors.New("connect to Cymonkey control endpoint")
	}
	defer connection.Close()
	connection.SetReadLimit(maxControlMessageBytes)
	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			return errors.New("read Cymonkey control request")
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage || len(payload) > maxControlMessageBytes {
			return errors.New("invalid Cymonkey control request")
		}
		var request controlRequest
		if err := json.Unmarshal(payload, &request); err != nil || strings.TrimSpace(request.Method) == "" || (len(request.Params) != 0 && !json.Valid(request.Params)) {
			return errors.New("invalid Cymonkey control request")
		}
		if len(bytes.TrimSpace(request.Params)) == 0 {
			request.Params = json.RawMessage(`{}`)
		}
		if err := connection.WriteJSON(runtime.handle(request)); err != nil {
			return errors.New("write Cymonkey control response")
		}
	}
}
