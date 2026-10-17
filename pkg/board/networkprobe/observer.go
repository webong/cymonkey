// Package networkprobe contains the declared HTTP health-probe mechanics
// for caller-approved HTTP(S) and TCP endpoints.
package networkprobe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// TCPDialer is the small native socket boundary used by a declared TCP
// observation. The host authorizes the destination before it reaches
// this dialer.
type TCPDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type NativeHTTPClient struct {
	Client *http.Client
}

type NativeTCPDialer struct {
	Dialer *net.Dialer
}

func (c NativeHTTPClient) Do(request *http.Request) (*http.Response, error) {
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	// The caller authorizes the exact destination in the probe request.
	// Clone the client so a redirect cannot silently expand that authorization
	// to a different scheme, host, or port. Preserve the caller's redirect
	// policy after this destination boundary has been checked.
	if request == nil || request.URL == nil {
		return nil, errors.New("network request is required")
	}
	authorized := *request.URL
	bounded := *client
	redirectPolicy := client.CheckRedirect
	bounded.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("network probe exceeded redirect limit")
		}
		if next == nil || next.URL == nil || !sameAuthority(&authorized, next.URL) {
			return errors.New("network redirect changed the authorized destination")
		}
		if redirectPolicy != nil {
			return redirectPolicy(next, via)
		}
		return nil
	}
	return bounded.Do(request)
}

func (d NativeTCPDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := d.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 3 * time.Second}
	}
	return dialer.DialContext(ctx, network, address)
}

func sameAuthority(first, second *url.URL) bool {
	if first == nil || second == nil {
		return false
	}
	return strings.EqualFold(first.Scheme, second.Scheme) &&
		strings.EqualFold(first.Hostname(), second.Hostname()) &&
		effectivePort(first) == effectivePort(second)
}

func effectivePort(value *url.URL) string {
	if value == nil {
		return ""
	}
	if port := value.Port(); port != "" {
		return port
	}
	switch strings.ToLower(value.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

type Config struct {
	Client HTTPClient
	Dialer TCPDialer
	Now    func() time.Time
}

type Probe struct{ config Config }

func New(config Config) Probe {
	if config.Client == nil {
		config.Client = NativeHTTPClient{Client: &http.Client{Timeout: 3 * time.Second}}
	}
	if config.Dialer == nil {
		config.Dialer = NativeTCPDialer{}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return Probe{config: config}
}

func (p Probe) Observe(ctx context.Context, input Request) (Response, error) {
	if err := input.Validate(); err != nil {
		return Response{}, err
	}
	switch input.Operation {
	case OperationProbe:
		return p.observeHTTP(ctx, input)
	case OperationProbeTCP:
		return p.observeTCP(ctx, input)
	default:
		return Response{}, fmt.Errorf("unsupported network observation %q", input.Operation)
	}
}

func (p Probe) observeHTTP(ctx context.Context, input Request) (Response, error) {
	started := p.config.Now()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, input.URL, nil)
	if err != nil {
		return Response{}, fmt.Errorf("network observation URL is invalid: %w", err)
	}
	response, err := p.config.Client.Do(httpRequest)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{

			Healthy: false, Message: "HTTP network probe failed",
		}, nil
	}
	if response == nil {
		return Response{

			Healthy: false, Message: "HTTP network probe returned no response",
		}, nil
	}
	if response.Body != nil {
		// Health depends only on the status; never drain an unbounded body.
		_ = response.Body.Close()
	}
	healthy := response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusBadRequest
	return Response{
		Healthy:    healthy,
		StatusCode: response.StatusCode, LatencyMillis: p.config.Now().Sub(started).Milliseconds(),
		Message: message(healthy, response.StatusCode),
	}, nil
}

func (p Probe) observeTCP(ctx context.Context, input Request) (Response, error) {
	started := p.config.Now()
	connection, err := p.config.Dialer.DialContext(ctx, "tcp", input.Address)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{

			Healthy: false, LatencyMillis: p.config.Now().Sub(started).Milliseconds(), Message: "TCP network probe failed",
		}, nil
	}
	_ = connection.Close()
	return Response{

		Healthy: true, LatencyMillis: p.config.Now().Sub(started).Milliseconds(), Message: "TCP network probe passed",
	}, nil
}

func message(healthy bool, statusCode int) string {
	if healthy {
		return "HTTP network probe passed"
	}
	return fmt.Sprintf("HTTP network probe returned %d", statusCode)
}
