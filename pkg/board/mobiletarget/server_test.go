package mobiletarget

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"os/exec"
	"testing"
	"time"

	"jangolova/devicekit"
	"jangolova/mobilekit"

	"github.com/gorilla/websocket"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
)

func startServer(t *testing.T, media mobilekit.Media) (*Server, mobilekit.StartResult) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	server := New()
	t.Cleanup(server.Close)
	raw, _ := json.Marshal(mobilekit.StartRequest{ID: "mobile-test", Media: media, TTLSeconds: 60})
	result, err := server.Call(context.Background(), "mobile.target.start", raw)
	if err != nil {
		t.Fatal(err)
	}
	return server, result.(mobilekit.StartResult)
}

func dial(t *testing.T, result mobilekit.StartResult, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	return websocket.DefaultDialer.Dial("ws://"+result.Endpoint.Address+result.Endpoint.Path, header)
}

func TestStartRefusesBadOptions(t *testing.T) {
	server := New()
	defer server.Close()
	for name, request := range map[string]mobilekit.StartRequest{
		"no media":    {ID: "x", TTLSeconds: 60},
		"no lifetime": {ID: "x", Media: mobilekit.Media{Video: true}},
		"long":        {ID: "x", Media: mobilekit.Media{Video: true}, TTLSeconds: 7200},
		"no id":       {Media: mobilekit.Media{Video: true}, TTLSeconds: 60},
	} {
		raw, _ := json.Marshal(request)
		_, err := server.Call(context.Background(), "mobile.target.start", raw)
		var remote *plugin.RemoteError
		if !asRemote(err, &remote) || remote.Code != mobilekit.CodeInvalidTarget {
			t.Errorf("%s: start = %v", name, err)
		}
	}
}

func asRemote(err error, target **plugin.RemoteError) bool {
	remote, ok := err.(*plugin.RemoteError)
	*target = remote
	return ok
}

func TestIngestAcceptsOneAuthenticatedAppAndValidatesFrames(t *testing.T) {
	_, result := startServer(t, mobilekit.Media{Video: true, Audio: true})
	if _, response, err := dial(t, result, "wrong"); err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: %v %v", response, err)
	}
	if _, _, err := dial(t, result, ""); err == nil {
		t.Fatal("an unauthenticated app attached")
	}
	conn, _, err := dial(t, result, result.Endpoint.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, response, err := dial(t, result, result.Endpoint.Token); err == nil || response == nil || response.StatusCode != http.StatusConflict {
		t.Fatalf("second attach: %v %v", response, err)
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, image.NewGray(image.Rect(0, 0, 32, 32)), nil); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, append([]byte{mobilekit.KindVideo}, buffer.Bytes()...)); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, append([]byte{mobilekit.KindMicrophone}, make([]byte, 8)...)); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("a frame of a kind that was not granted kept the attachment open")
	}
}

func TestViewerHalfListsThePeerThroughTheContract(t *testing.T) {
	server, _ := startServer(t, mobilekit.Media{Video: true})
	result, err := server.Call(context.Background(), "mobile.target.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	devices := result.([]devicekit.Device)
	if len(devices) != 1 || devices[0].ID != "peer" || devices[0].ProviderID != "mobile-test" {
		t.Fatalf("devices = %+v", devices)
	}
	have := map[devicekit.Capability]bool{}
	for _, c := range devices[0].Capabilities {
		have[c.Name] = true
	}
	if !have[devicekit.WebRTCSignal] || !have[devicekit.DisplayVideo] || have[devicekit.AudioCapture] {
		t.Fatalf("capabilities = %v", have)
	}
}

func TestStopEndsTheIngest(t *testing.T) {
	server, result := startServer(t, mobilekit.Media{Video: true})
	if _, err := server.Call(context.Background(), "mobile.target.stop", nil); err != nil {
		t.Fatal(err)
	}
	if conn, _, err := dial(t, result, result.Endpoint.Token); err == nil {
		conn.Close()
		t.Fatal("the ingest still accepted a connection after stop")
	}
}
