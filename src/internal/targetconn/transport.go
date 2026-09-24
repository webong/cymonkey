package targetconn

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"cymonkey/src/internal/orchestrator"
)

// HTTPClient returns an isolated transport that applies resolved headers and
// TLS material. It never mutates http.DefaultTransport.
func HTTPClient(endpoint orchestrator.TargetEndpoint, timeout time.Duration) (*http.Client, error) {
	if err := Validate(endpoint); err != nil {
		return nil, err
	}
	snapshot := endpoint.Connection.Snapshot()
	transport, err := httpTransport(endpoint.URL, snapshot.TLS)
	if err != nil {
		return nil, err
	}
	var roundTripper http.RoundTripper = transport
	if endpoint.Connection != nil {
		roundTripper = &materialTransport{
			base: transport, endpointURL: endpoint.URL, material: endpoint.Connection,
			activeTLSRevision: snapshot.TLSRevision,
		}
	}
	return &http.Client{Timeout: timeout, Transport: roundTripper}, nil
}

// WebSocketDialer returns an isolated dialer and authentication headers from
// one connection-material snapshot. Callers supply the headers only to the
// WebSocket handshake request.
func WebSocketDialer(endpoint orchestrator.TargetEndpoint) (*websocket.Dialer, map[string]string, error) {
	if err := Validate(endpoint); err != nil {
		return nil, nil, err
	}
	parsed, err := url.Parse(endpoint.URL)
	if err != nil || parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return nil, nil, errors.New("WebSocket endpoint must use ws or wss")
	}
	dialer := *websocket.DefaultDialer
	snapshot := endpoint.Connection.Snapshot()
	if snapshot.TLS != nil {
		if parsed.Scheme != "wss" {
			return nil, nil, errors.New("TLS connection material requires a wss endpoint")
		}
		dialer.TLSClientConfig, err = tlsConfig(snapshot.TLS)
		if err != nil {
			return nil, nil, err
		}
	}
	return &dialer, snapshot.Headers, nil
}

// Headers returns a defensive copy suitable for an adapter-private connection
// handshake.
func Headers(endpoint orchestrator.TargetEndpoint) map[string]string {
	if endpoint.Connection == nil {
		return nil
	}
	return endpoint.Connection.Snapshot().Headers
}

type materialTransport struct {
	stateMu           sync.RWMutex
	rotationMu        sync.Mutex
	base              *http.Transport
	endpointURL       string
	material          *orchestrator.EndpointConnection
	activeTLSRevision uint64
}

func (t *materialTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	snapshot := t.material.Snapshot()
	if !snapshot.ExpiresAt.IsZero() && !snapshot.ExpiresAt.After(time.Now()) {
		return nil, errors.New("target connection material has expired")
	}
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	for name, value := range snapshot.Headers {
		cloned.Header.Set(name, value)
	}

	t.stateMu.RLock()
	if snapshot.TLSRevision == t.activeTLSRevision {
		response, err := t.base.RoundTrip(cloned)
		t.stateMu.RUnlock()
		return response, err
	}
	t.stateMu.RUnlock()

	// Only one request validates and promotes a TLS generation. The active
	// transport remains available until the candidate completes a real request.
	t.rotationMu.Lock()
	defer t.rotationMu.Unlock()
	snapshot = t.material.Snapshot()
	if !snapshot.ExpiresAt.IsZero() && !snapshot.ExpiresAt.After(time.Now()) {
		return nil, errors.New("target connection material has expired")
	}
	cloned = request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	for name, value := range snapshot.Headers {
		cloned.Header.Set(name, value)
	}
	t.stateMu.RLock()
	if snapshot.TLSRevision == t.activeTLSRevision {
		response, err := t.base.RoundTrip(cloned)
		t.stateMu.RUnlock()
		return response, err
	}
	t.stateMu.RUnlock()
	candidate, err := httpTransport(t.endpointURL, snapshot.TLS)
	if err != nil {
		return nil, err
	}
	response, err := candidate.RoundTrip(cloned)
	if err != nil {
		candidate.CloseIdleConnections()
		return nil, err
	}
	t.stateMu.Lock()
	previous := t.base
	t.base = candidate
	t.activeTLSRevision = snapshot.TLSRevision
	t.stateMu.Unlock()
	previous.CloseIdleConnections()
	t.material.Acknowledge(snapshot.Revision)
	return response, nil
}

func (t *materialTransport) CloseIdleConnections() {
	t.stateMu.RLock()
	t.base.CloseIdleConnections()
	t.stateMu.RUnlock()
}

func httpTransport(endpointURL string, material *orchestrator.TLSConnection) (*http.Transport, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if material == nil {
		return transport, nil
	}
	parsed, err := url.Parse(endpointURL)
	if err != nil || parsed.Scheme != "https" {
		return nil, errors.New("TLS connection material requires an https endpoint")
	}
	config, err := tlsConfig(material)
	if err != nil {
		return nil, err
	}
	transport.TLSClientConfig = config
	return transport, nil
}

func tlsConfig(material *orchestrator.TLSConnection) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: material.ServerName}
	if material.CAFile != "" {
		contents, err := os.ReadFile(material.CAFile)
		if err != nil {
			return nil, errors.New("read target TLS CA file")
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(contents) {
			return nil, errors.New("target TLS CA file contains no certificates")
		}
		config.RootCAs = roots
	}
	if material.ClientCertificateFile != "" {
		certificate, err := tls.LoadX509KeyPair(material.ClientCertificateFile, material.ClientKeyFile)
		if err != nil {
			return nil, errors.New("load target TLS client certificate")
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}

// NodeEnvironment configures a child Node.js process for a private CA. CDP
// worker libraries accept headers but do not expose a portable client-certificate
// hook, so mTLS is rejected explicitly instead of being silently ignored.
func NodeEnvironment(endpoint orchestrator.TargetEndpoint, current []string) ([]string, error) {
	if err := Validate(endpoint); err != nil {
		return nil, err
	}
	if endpoint.Connection == nil {
		return current, nil
	}
	material := endpoint.Connection.Snapshot().TLS
	if material == nil {
		return current, nil
	}
	if material.ClientCertificateFile != "" {
		return nil, errors.New("CDP worker does not support TLS client certificates")
	}
	parsed, err := url.Parse(endpoint.URL)
	if err != nil || parsed.Scheme != "wss" && parsed.Scheme != "https" {
		return nil, errors.New("TLS connection material requires a wss or https CDP endpoint")
	}
	if material.ServerName != "" && !strings.EqualFold(material.ServerName, parsed.Hostname()) {
		return nil, fmt.Errorf("CDP worker TLS serverName must match endpoint hostname %q", parsed.Hostname())
	}
	if material.CAFile == "" {
		return current, nil
	}
	return replaceEnvironment(current, "NODE_EXTRA_CA_CERTS", material.CAFile), nil
}

func replaceEnvironment(current []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(current)+1)
	for _, item := range current {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

func RedactString(message string, target orchestrator.EngineTarget) string {
	for _, secret := range stableSecretValues(target) {
		message = strings.ReplaceAll(message, secret, "[REDACTED]")
	}
	return message
}

// RedactJSON walks JSON string values so secrets containing JSON escape
// characters are still removed without corrupting the response document.
func RedactJSON(document json.RawMessage, target orchestrator.EngineTarget) json.RawMessage {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(document)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return json.RawMessage(RedactString(string(document), target))
	}
	redactJSONValue(value, target)
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return encoded
}

func redactJSONValue(value any, target orchestrator.EngineTarget) {
	switch typed := value.(type) {
	case map[string]any:
		for name, item := range typed {
			if text, ok := item.(string); ok {
				typed[name] = RedactString(text, target)
			} else {
				redactJSONValue(item, target)
			}
		}
	case []any:
		for index, item := range typed {
			if text, ok := item.(string); ok {
				typed[index] = RedactString(text, target)
			} else {
				redactJSONValue(item, target)
			}
		}
	}
}

type redactingError struct{ message string }

func (e redactingError) Error() string { return e.message }

func RedactContextError(err error, target orchestrator.EngineTarget) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return redactingError{message: RedactString(err.Error(), target)}
}
