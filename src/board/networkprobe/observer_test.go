package networkprobe

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(request *http.Request) (*http.Response, error) { return f(request) }

func TestObserveReturnsNormalizedHTTPResult(t *testing.T) {
	probe := New(Config{
		Client: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.String() != "http://localhost:9876/healthz" {
				t.Fatalf("unexpected URL %s", request.URL)
			}
			return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("private body"))}, nil
		}),
		Now: func() time.Time { return time.Unix(10, 0) },
	})
	value, err := probe.Observe(context.Background(), NewRequest("http://localhost:9876/healthz"))
	if err != nil || !value.Healthy || value.StatusCode != http.StatusNoContent || value.LatencyMillis != 0 {
		t.Fatalf("value=%+v err=%v", value, err)
	}
}

func TestObserveRejectsNonHTTPURL(t *testing.T) {
	_, err := New(Config{}).Observe(context.Background(), NewRequest("file:///tmp/private"))
	if err == nil {
		t.Fatal("expected non-HTTP URL to be rejected")
	}
}

func TestObserveRejectsURLCredentials(t *testing.T) {
	_, err := New(Config{}).Observe(context.Background(), NewRequest("https://user:secret@example.test/healthz"))
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("URL credentials were accepted: %v", err)
	}
}

func TestObserveReturnsNormalizedTCPResult(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = connection.Close()
		}
	}()

	value, err := New(Config{}).Observe(context.Background(), NewTCPRequest(listener.Addr().String()))
	if err != nil || !value.Healthy || value.Message != "TCP network probe passed" {
		t.Fatalf("value=%+v err=%v", value, err)
	}
}

func TestObserveReturnsBoundedTCPFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	value, err := New(Config{}).Observe(context.Background(), NewTCPRequest(address))
	if err != nil || value.Healthy || value.Message != "TCP network probe failed" {
		t.Fatalf("value=%+v err=%v", value, err)
	}
}

func TestNativeHTTPClientRejectsRedirectToDifferentAuthority(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", target.URL)
		writer.WriteHeader(http.StatusFound)
	}))
	defer redirect.Close()

	client := NativeHTTPClient{Client: &http.Client{Timeout: time.Second}}
	request, err := http.NewRequest(http.MethodGet, redirect.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil || !strings.Contains(err.Error(), "authorized destination") {
		t.Fatalf("redirect was not rejected: %v", err)
	}
}

func TestSameAuthorityAllowsPathRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/healthz" {
			http.Redirect(writer, request, "/ready", http.StatusFound)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	value, err := New(Config{Client: NativeHTTPClient{Client: &http.Client{Timeout: time.Second}}}).Observe(
		context.Background(), NewRequest(server.URL+"/healthz"),
	)
	if err != nil || !value.Healthy || value.StatusCode != http.StatusNoContent {
		t.Fatalf("same-authority redirect failed: value=%+v err=%v", value, err)
	}
}
