package networkprobe

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type unreadableBody struct{ closed bool }

func (*unreadableBody) Read([]byte) (int, error) {
	panic("health probe must not drain a response body")
}
func (b *unreadableBody) Close() error { b.closed = true; return nil }

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProbeClosesBodyWithoutReading(t *testing.T) {
	body := &unreadableBody{}
	p := New(Config{Client: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body}, nil
	})})
	result, err := p.Observe(context.Background(), NewRequest("http://example.test/health"))
	if err != nil || !result.Healthy || !body.closed {
		t.Fatalf("result=%+v closed=%v err=%v", result, body.closed, err)
	}
}

func TestProbeReturnsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := New(Config{Client: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, ctx.Err() })})
	if _, err := p.Observe(ctx, NewRequest("http://example.test/health")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestProbeBoundsSameAuthorityRedirectLoop(t *testing.T) {
	client := NativeHTTPClient{Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"/again"}}, Body: http.NoBody, Request: r}, nil
	})}}
	request, _ := http.NewRequest(http.MethodGet, "http://example.test/again", nil)
	if _, err := client.Do(request); err == nil || !strings.Contains(err.Error(), "redirect limit") {
		t.Fatalf("err=%v", err)
	}
}
