// Command mobile-target-harness runs one mobile target test by hand: it
// registers a target and a viewer session with a running operator, exposes only
// the phone's WebSocket path over TLS on the LAN, and serves the browser viewer
// on loopback with its credentials prefilled.
//
//	export CYMONKEY_OPERATOR_TOKEN=...            # the running operator's token
//	go run ./scripts/mobile-target-harness --cert c.pem --key k.pem
//
// The operator stays on loopback. The phone reaches nothing but the target's own
// path; the operator token never leaves this process. Credentials are printed to
// the terminal for the phone and handed to the viewer page from loopback only.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mobile-target-harness:", err)
		os.Exit(1)
	}
}

type operatorClient struct {
	base  *url.URL
	token string
}

func (c operatorClient) do(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, c.base.String()+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %d %s", method, path, response.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func run() error {
	operatorURL := flag.String("operator", "http://127.0.0.1:7395", "operator URL (loopback)")
	host := flag.String("host", "", "address the phone uses to reach this Mac (default: first LAN IPv4)")
	listen := flag.String("listen", ":8443", "TLS listen address for the phone")
	certFile := flag.String("cert", "", "TLS certificate for --host (for example from mkcert)")
	keyFile := flag.String("key", "", "TLS key for the certificate")
	viewerListen := flag.String("viewer-listen", "127.0.0.1:7400", "loopback address for the browser viewer")
	viewerDir := flag.String("viewer-dir", "lib/device-viewer", "viewer static files")
	ttl := flag.Int("ttl", 900, "target and session lifetime in seconds")
	audio := flag.Bool("audio", false, "grant system audio")
	microphone := flag.Bool("microphone", false, "grant the phone microphone")
	playback := flag.Bool("playback", false, "grant audio playback to the phone")
	copyToken := flag.Bool("copy-token", true, "copy the phone token to the macOS clipboard")
	flag.Parse()
	token := strings.TrimSpace(os.Getenv("CYMONKEY_OPERATOR_TOKEN"))
	if token == "" || *certFile == "" || *keyFile == "" {
		return errors.New("CYMONKEY_OPERATOR_TOKEN, --cert and --key are required")
	}
	base, err := url.Parse(*operatorURL)
	if err != nil || base.Hostname() != "127.0.0.1" && base.Hostname() != "localhost" {
		return errors.New("--operator must be a loopback URL")
	}
	if *host == "" {
		if *host, err = lanAddress(); err != nil {
			return err
		}
	}
	certificate, err := tls.LoadX509KeyPair(*certFile, *keyFile)
	if err != nil {
		return err
	}
	operator := operatorClient{base: base, token: token}

	var target struct {
		AdapterID string `json:"adapterId"`
		Path      string `json:"path"`
		Token     string `json:"token"`
	}
	if err := operator.do("POST", "/v1/device-targets", map[string]any{"name": "harness", "video": true, "audio": *audio, "microphone": *microphone, "playback": *playback, "ttlSeconds": *ttl}, &target); err != nil {
		return fmt.Errorf("register target: %w", err)
	}
	capabilities := []string{"webrtc.signal", "webrtc.poll", "display.video"}
	if *audio {
		capabilities = append(capabilities, "audio.capture")
	}
	if *microphone {
		capabilities = append(capabilities, "audio.microphone")
	}
	if *playback {
		capabilities = append(capabilities, "audio.playback")
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	if err := operator.do("POST", "/v1/device-sessions", map[string]any{"open": map[string]any{"adapterId": target.AdapterID, "deviceId": "peer", "grant": map[string]any{"capabilities": capabilities}}, "ttlSeconds": *ttl}, &session); err != nil {
		_ = operator.do("DELETE", target.Path, nil, nil)
		return fmt.Errorf("open viewer session: %w", err)
	}
	var viewer struct {
		Path  string `json:"path"`
		Token string `json:"token"`
	}
	if err := operator.do("POST", "/v1/device-sessions/"+session.SessionID+"/viewer", nil, &viewer); err != nil {
		return fmt.Errorf("issue viewer credential: %w", err)
	}
	cleanup := func() {
		_ = operator.do("DELETE", "/v1/device-sessions/"+session.SessionID, nil, nil)
		_ = operator.do("DELETE", target.Path, nil, nil)
	}
	defer cleanup()

	proxy := httputil.NewSingleHostReverseProxy(base)
	// The phone gets exactly its target's path and nothing else of the operator.
	phone := http.NewServeMux()
	phone.Handle(target.Path, proxy)
	phoneServer := &http.Server{Addr: *listen, Handler: phone, TLSConfig: &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}, ReadHeaderTimeout: 5 * time.Second}

	static := http.FileServer(http.Dir(*viewerDir))
	page := http.NewServeMux()
	page.Handle("/v1/device-viewer/", proxy)
	page.HandleFunc("/__harness/creds.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]string{"endpoint": viewer.Path, "credential": viewer.Token})
	})
	page.HandleFunc("/__harness.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, `const c=await (await fetch('/__harness/creds.json')).json();
document.getElementById('endpoint').value=c.endpoint;document.getElementById('credential').value=c.credential;
document.getElementById('status').textContent='Credentials loaded; press Connect.';`)
	})
	page.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			static.ServeHTTP(w, r)
			return
		}
		html, err := os.ReadFile(filepath.Join(*viewerDir, "index.html"))
		if err != nil {
			http.Error(w, "viewer page unavailable", 500)
			return
		}
		html = bytes.Replace(html, []byte("</html>"), []byte(`<script type="module" src="/__harness.js"></script></html>`), 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	if h, _, err := net.SplitHostPort(*viewerListen); err != nil || (h != "127.0.0.1" && h != "localhost") {
		return errors.New("--viewer-listen must be loopback")
	}
	viewerServer := &http.Server{Addr: *viewerListen, Handler: page, ReadHeaderTimeout: 5 * time.Second}

	errs := make(chan error, 2)
	go func() { errs <- phoneServer.ListenAndServeTLS("", "") }()
	go func() { errs <- viewerServer.ListenAndServe() }()
	_, port, _ := net.SplitHostPort(*listen)

	if *copyToken {
		if copyCmd := exec.Command("pbcopy"); copyCmd.Err == nil {
			copyCmd.Stdin = strings.NewReader(target.Token)
			_ = copyCmd.Run()
		}
	}
	fmt.Printf("\nPhone (Board reference app):\n  endpoint  wss://%s:%s%s\n  token     %s\n", *host, port, target.Path, target.Token)
	if *copyToken {
		fmt.Println("            (token is on the Mac clipboard: paste it on the phone with Universal Clipboard)")
	}
	fmt.Printf("\nViewer (this Mac):\n  open      http://%s/  then press Connect\n", *viewerListen)
	fmt.Printf("\nTarget lifetime %ds. Press Ctrl-C to end the target and the viewer session.\n", *ttl)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-stop:
	case err := <-errs:
		fmt.Fprintln(os.Stderr, "server stopped:", err)
	case <-time.After(time.Duration(*ttl) * time.Second):
		fmt.Println("lifetime elapsed")
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = phoneServer.Shutdown(shutdown)
	_ = viewerServer.Shutdown(shutdown)
	return nil
}

func lanAddress() (string, error) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, address := range addresses {
		if network, ok := address.(*net.IPNet); ok && network.IP.To4() != nil && !network.IP.IsLoopback() && !network.IP.IsLinkLocalUnicast() {
			return network.IP.String(), nil
		}
	}
	return "", errors.New("no LAN IPv4 address; pass --host")
}
