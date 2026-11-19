package networkprobe

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	// OperationProbe checks a declared HTTP(S) endpoint.
	OperationProbe = "probe"
	// OperationProbeTCP observes one exact TCP endpoint. The endpoint is
	// authorized by the caller; the response never exposes it.
	OperationProbeTCP = "probe.tcp"
)

type Request struct {
	Operation string `json:"operation"`
	URL       string `json:"url,omitempty"`
	Address   string `json:"address,omitempty"`
}

type Response struct {
	Healthy       bool   `json:"healthy"`
	StatusCode    int    `json:"statusCode,omitempty"`
	LatencyMillis int64  `json:"latencyMillis,omitempty"`
	Message       string `json:"message,omitempty"`
}

func NewRequest(targetURL string) Request {
	return Request{Operation: OperationProbe, URL: targetURL}
}

// NewTCPRequest creates a bounded TCP reachability observation. Address must
// be a concrete host:port pair; no URL, credentials, or request payload is
// part of this contract.
func NewTCPRequest(address string) Request {
	return Request{Operation: OperationProbeTCP, Address: address}
}

func (r Request) Validate() error {
	switch r.Operation {
	case OperationProbe:
		if strings.TrimSpace(r.Address) != "" {
			return fmt.Errorf("HTTP network probe must not include a TCP address")
		}
		parsed, err := url.Parse(strings.TrimSpace(r.URL))
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("network URL must be an absolute HTTP or HTTPS URL")
		}
		if parsed.User != nil {
			return fmt.Errorf("network URL must not contain credentials")
		}
	case OperationProbeTCP:
		if strings.TrimSpace(r.URL) != "" {
			return fmt.Errorf("TCP network probe must not include a URL")
		}
		if err := validateTCPAddress(r.Address); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported network operation %q", r.Operation)
	}
	return nil
}

func validateTCPAddress(value string) error {
	host, rawPort, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil || strings.TrimSpace(host) == "" {
		return fmt.Errorf("TCP network address must be a concrete host:port pair")
	}
	if strings.ContainsAny(host, "\x00/\\?#@ \t\r\n") {
		return fmt.Errorf("TCP network address contains an invalid host")
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("TCP network address contains an invalid port")
	}
	return nil
}
