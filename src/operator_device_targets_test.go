package main

import (
	"bytes"
	"context"
	"cymonkey/src/internal/orchestrator"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"jangolova/devicekit"
	"jangolova/mobilekit"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
)

// fakeMobileGuest stands in for the mobile-target package: a WebSocket server
// that records the frames the host relays and can send frames back.
type fakeMobileGuest struct {
	server   *httptest.Server
	frames   chan []byte
	conn     chan *websocket.Conn
	done     chan struct{}
	doneOnce sync.Once
	closed   chan struct{}
}

func newFakeMobileGuest(t *testing.T) *fakeMobileGuest {
	t.Helper()
	g := &fakeMobileGuest{frames: make(chan []byte, 16), conn: make(chan *websocket.Conn, 1), done: make(chan struct{}), closed: make(chan struct{})}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		g.conn <- conn
		for {
			_, frame, err := conn.ReadMessage()
			if err != nil {
				return
			}
			g.frames <- frame
		}
	}))
	t.Cleanup(g.server.Close)
	return g
}

func (g *fakeMobileGuest) Provider() devicekit.Provider { return operatorDeviceFixture{} }
func (g *fakeMobileGuest) Dial(ctx context.Context) (*websocket.Conn, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(g.server.URL, "http"), nil)
	return conn, err
}
func (g *fakeMobileGuest) Done() <-chan struct{} { return g.done }
func (g *fakeMobileGuest) Close() error {
	g.doneOnce.Do(func() { close(g.done); close(g.closed) })
	return nil
}

type mobileHarness struct {
	server *operatorServer
	http   *httptest.Server
	guest  *fakeMobileGuest
	start  mobilekit.StartRequest
}

func newMobileHarness(t *testing.T) *mobileHarness {
	t.Helper()
	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close(context.Background()) })
	h := &mobileHarness{server: server, guest: newFakeMobileGuest(t)}
	server.mobileStart = func(request mobilekit.StartRequest) (mobileBackend, error) {
		h.start = request
		return h.guest, nil
	}
	h.http = httptest.NewServer(server.Routes())
	t.Cleanup(h.http.Close)
	return h
}

type createdTarget struct {
	AdapterID string `json:"adapterId"`
	Path      string `json:"path"`
	Token     string `json:"token"`
}

func (h *mobileHarness) create(t *testing.T, body string) createdTarget {
	t.Helper()
	response := operatorTestRequest(h.server, "secret", http.MethodPost, "/v1/device-targets", body)
	if response.Code != 201 {
		t.Fatalf("create %d %s", response.Code, response.Body.String())
	}
	var created createdTarget
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	return created
}

func (h *mobileHarness) attach(t *testing.T, created createdTarget, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.http.URL, "http")+created.Path, http.Header{"Authorization": []string{"Bearer " + token}})
}

func jpegFrame(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, image.NewGray(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatal(err)
	}
	return append([]byte{mobilekit.KindVideo}, buffer.Bytes()...)
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for " + what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *mobileHarness) targets() int {
	h.server.mu.Lock()
	defer h.server.mu.Unlock()
	return len(h.server.mobileTargets)
}

func TestMobileTargetStartsThePackageWithPlainOptions(t *testing.T) {
	h := newMobileHarness(t)
	created := h.create(t, `{"name":"phone","video":true,"playback":true,"ttlSeconds":120,"relayOnly":true,"iceServers":[{"urls":["turn:relay.example"],"username":"u","credential":"c"}]}`)
	if !strings.HasPrefix(created.AdapterID, "mobile-") || len(created.Token) != 64 {
		t.Fatalf("created %+v", created)
	}
	want := mobilekit.StartRequest{ID: created.AdapterID, Name: "phone", RelayOnly: true, Media: mobilekit.Media{Video: true, Playback: true}, TTLSeconds: 120,
		ICEServers: []mobilekit.ICEServer{{URLs: []string{"turn:relay.example"}, Username: "u", Credential: "c"}}}
	got, _ := json.Marshal(h.start)
	expected, _ := json.Marshal(want)
	if string(got) != string(expected) {
		t.Fatalf("start request %s, want %s", got, expected)
	}
}

func TestMobileTargetRefusals(t *testing.T) {
	h := newMobileHarness(t)
	for name, body := range map[string]string{
		"no media":      `{"ttlSeconds":60}`,
		"long lifetime": `{"video":true,"ttlSeconds":7200}`,
	} {
		if response := operatorTestRequest(h.server, "secret", http.MethodPost, "/v1/device-targets", body); response.Code != 422 {
			t.Errorf("%s: %d %s", name, response.Code, response.Body.String())
		}
	}
	for name, test := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"ffmpeg missing":  {mobilekit.ErrConverterUnavailable, 503, "converter_unavailable"},
		"package refused": {mobilekit.ErrInvalidTarget, 422, "invalid_target"},
		"no package":      {errNoMobileTargetPackage, 503, "mobile_target_unavailable"},
		"package failed":  {errors.New("secret detail"), 502, "mobile_target_failed"},
	} {
		h.server.mobileStart = func(mobilekit.StartRequest) (mobileBackend, error) { return nil, test.err }
		response := operatorTestRequest(h.server, "secret", http.MethodPost, "/v1/device-targets", `{"video":true}`)
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.code) || strings.Contains(response.Body.String(), "secret detail") {
			t.Errorf("%s: %d %s", name, response.Code, response.Body.String())
		}
	}
}

func TestMobileTargetWithoutAPackageAnswers503(t *testing.T) {
	t.Setenv("CYMONKEY_PLUGIN_DIR", t.TempDir())
	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	response := operatorTestRequest(server, "secret", http.MethodPost, "/v1/device-targets", `{"video":true}`)
	if response.Code != 503 || !strings.Contains(response.Body.String(), "mobile_target_unavailable") {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
}

func TestMobileTargetRelaysValidFramesAndPlayback(t *testing.T) {
	h := newMobileHarness(t)
	created := h.create(t, `{"video":true,"microphone":true,"playback":true}`)
	if _, response, err := h.attach(t, created, strings.Repeat("0", 64)); err == nil || response == nil || response.StatusCode != 401 {
		t.Fatalf("wrong credential: %v %v", response, err)
	}
	app, _, err := h.attach(t, created, created.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if _, response, err := h.attach(t, created, created.Token); err == nil || response == nil || response.StatusCode != 401 {
		t.Fatalf("second attach: %v %v", response, err)
	}
	video := jpegFrame(t, 64, 48)
	microphone := append([]byte{mobilekit.KindMicrophone}, make([]byte, 16)...)
	for _, frame := range [][]byte{video, microphone} {
		if err := app.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-h.guest.frames:
			if !bytes.Equal(got, frame) {
				t.Fatal("the relayed frame differs")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("frame was not relayed")
		}
	}
	guest := <-h.guest.conn
	playback := append([]byte{mobilekit.KindPlayback}, make([]byte, 64)...)
	if err := guest.WriteMessage(websocket.BinaryMessage, playback); err != nil {
		t.Fatal(err)
	}
	app.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, got, err := app.ReadMessage(); err != nil || !bytes.Equal(got, playback) {
		t.Fatalf("playback %v %v", got, err)
	}
}

func TestMobileTargetDropsAnAppThatSendsAnInvalidFrame(t *testing.T) {
	for name, frame := range map[string]func(*testing.T) []byte{
		"too large in pixels": func(t *testing.T) []byte { return jpegFrame(t, 4000, 3000) },
		"not a jpeg":          func(*testing.T) []byte { return []byte{mobilekit.KindVideo, 1, 2, 3} },
		"kind not granted":    func(*testing.T) []byte { return append([]byte{mobilekit.KindAudio}, make([]byte, 8)...) },
		"inbound playback":    func(*testing.T) []byte { return append([]byte{mobilekit.KindPlayback}, make([]byte, 8)...) },
		"too short":           func(*testing.T) []byte { return []byte{mobilekit.KindVideo} },
	} {
		t.Run(name, func(t *testing.T) {
			h := newMobileHarness(t)
			created := h.create(t, `{"video":true}`)
			app, _, err := h.attach(t, created, created.Token)
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()
			if err := app.WriteMessage(websocket.BinaryMessage, frame(t)); err != nil {
				t.Fatal(err)
			}
			app.SetReadDeadline(time.Now().Add(3 * time.Second))
			if _, _, err := app.ReadMessage(); err == nil {
				t.Fatal("the attachment stayed open")
			}
			select {
			case got := <-h.guest.frames:
				t.Fatalf("the guest received a rejected frame of %d bytes", len(got))
			default:
			}
			waitFor(t, "target removal", func() bool { return h.targets() == 0 })
			<-h.guest.closed
		})
	}
}

func TestMobileTargetEndsWhenThePackageProcessDies(t *testing.T) {
	h := newMobileHarness(t)
	created := h.create(t, `{"video":true}`)
	app, _, err := h.attach(t, created, created.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	h.guest.Close()
	waitFor(t, "target removal", func() bool { return h.targets() == 0 })
	app.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := app.ReadMessage(); err == nil {
		t.Fatal("the app stayed attached to a dead target")
	}
}

func TestMobileTargetDeleteStopsThePackage(t *testing.T) {
	h := newMobileHarness(t)
	created := h.create(t, `{"video":true}`)
	if response := operatorTestRequest(h.server, "secret", http.MethodDelete, created.Path, ""); response.Code != 204 {
		t.Fatalf("delete %d", response.Code)
	}
	select {
	case <-h.guest.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("the package was not stopped")
	}
	if h.targets() != 0 {
		t.Fatal("the target remained")
	}
}

func TestMobileTargetLimit(t *testing.T) {
	h := newMobileHarness(t)
	for i := 0; i < maxMobileTargets; i++ {
		h.server.mobileStart = func(mobilekit.StartRequest) (mobileBackend, error) { return newFakeMobileGuest(t), nil }
		h.create(t, `{"video":true}`)
	}
	if response := operatorTestRequest(h.server, "secret", http.MethodPost, "/v1/device-targets", `{"video":true}`); response.Code != 503 {
		t.Fatalf("over the limit: %d", response.Code)
	}
}

// TestMobileTargetPackageEndToEnd builds the real mobile-target package,
// installs it through ext and runs a target through the operator API: the app
// attaches and streams, a viewer session opens on the live process, the host
// ends the process with the target.
func TestMobileTargetPackageEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	root := t.TempDir()
	t.Setenv("CYMONKEY_PLUGIN_DIR", filepath.Join(root, "installed"))
	source := filepath.Join(root, "mobile")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(source, "mobile")
	buildGuest(t, executable, "pkg/board/cmd/mobile")
	manifest, err := packagekit.ExecutableManifest("mobile", "1.0.0", []plugin.Contract{pluginContract(mobileTargetContract)}, executable)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(source, store.ManifestName)
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pluginsCommand([]string{"install", "--manifest", manifestPath}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	// The package serves device operations under its own contract, so the
	// device provider discovery must not launch it.
	if providers, err := devicePluginAdapters(); err != nil || len(providers) != 0 {
		t.Fatalf("device discovery found the mobile package: %v %v", providers, err)
	}

	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	server.deviceProviders = func() ([]devicekit.Provider, error) { return nil, nil }
	listener := httptest.NewServer(server.Routes())
	defer listener.Close()

	response := operatorTestRequest(server, "secret", http.MethodPost, "/v1/device-targets", `{"video":true,"microphone":true,"ttlSeconds":120}`)
	if response.Code != 201 {
		t.Fatalf("create %d %s", response.Code, response.Body.String())
	}
	var created createdTarget
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	app, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(listener.URL, "http")+created.Path, http.Header{"Authorization": []string{"Bearer " + created.Token}})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	for i := 0; i < 5; i++ {
		if err := app.WriteMessage(websocket.BinaryMessage, jpegFrame(t, 64, 48)); err != nil {
			t.Fatal(err)
		}
	}

	opened := operatorTestRequest(server, "secret", http.MethodPost, "/v1/device-sessions", `{"open":{"adapterId":"`+created.AdapterID+`","deviceId":"peer","grant":{"capabilities":["device.health","webrtc.poll","webrtc.signal"]}},"ttlSeconds":60}`)
	if opened.Code != 201 {
		t.Fatalf("open viewer session %d %s", opened.Code, opened.Body.String())
	}
	var session struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	path := "/v1/device-sessions/" + session.SessionID
	if health := operatorTestRequest(server, "secret", http.MethodPost, path+"/actions", `{"capability":"device.health"}`); health.Code != 200 {
		t.Fatalf("health %d %s", health.Code, health.Body.String())
	}
	if denied := operatorTestRequest(server, "secret", http.MethodPost, path+"/actions", `{"capability":"display.video"}`); denied.Code != 403 {
		t.Fatalf("ungranted action %d", denied.Code)
	}
	if closed := operatorTestRequest(server, "secret", http.MethodDelete, path, ""); closed.Code != 204 {
		t.Fatalf("close viewer session %d", closed.Code)
	}
	// The target outlives the viewer: another can open on the same process.
	again := operatorTestRequest(server, "secret", http.MethodPost, "/v1/device-sessions", `{"open":{"adapterId":"`+created.AdapterID+`","deviceId":"peer","grant":{"capabilities":["device.health","webrtc.poll","webrtc.signal"]}},"ttlSeconds":60}`)
	if again.Code != 201 {
		t.Fatalf("second viewer %d %s", again.Code, again.Body.String())
	}

	// A frame the host refuses ends the attachment and the target with it.
	if err := app.WriteMessage(websocket.BinaryMessage, []byte{mobilekit.KindVideo, 1, 2}); err != nil {
		t.Fatal(err)
	}
	app.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := app.ReadMessage(); err == nil {
		t.Fatal("the attachment survived an invalid frame")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		server.mu.Lock()
		n := len(server.mobileTargets)
		server.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the target remained after its attachment ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
