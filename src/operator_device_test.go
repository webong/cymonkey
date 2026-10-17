package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"devicekit"
	"cymonkey/src/internal/orchestrator"
)

type operatorDeviceFixture struct{}

func (operatorDeviceFixture) ID() string { return "fixture" }
func (operatorDeviceFixture) List(context.Context) ([]devicekit.Device, error) {
	return []devicekit.Device{{ID: "drive", Kind: "drive", Capabilities: []devicekit.CapabilityDescriptor{
		{Name: devicekit.DriveList, InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: devicekit.DriveRead, InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}}, nil
}
func (operatorDeviceFixture) Open(context.Context, string, devicekit.Grant) (devicekit.Session, error) {
	return operatorDeviceSession{}, nil
}

type operatorDeviceSession struct{}

func (operatorDeviceSession) Invoke(context.Context, devicekit.Action) (devicekit.Result, error) {
	return devicekit.Result{Output: json.RawMessage(`{"files":["image.jpg"]}`)}, nil
}
func (operatorDeviceSession) Stream(context.Context, devicekit.Action) (devicekit.Content, error) {
	return &operatorDeviceContent{Reader: bytes.NewReader([]byte("image"))}, nil
}
func (operatorDeviceSession) Close(context.Context) error { return nil }

type operatorDeviceContent struct{ *bytes.Reader }

func (*operatorDeviceContent) Size() int64     { return 5 }
func (*operatorDeviceContent) Truncated() bool { return false }
func (*operatorDeviceContent) Close() error    { return nil }

func TestOperatorDeviceUsesExplicitGrantForActionsAndStreams(t *testing.T) {
	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	server.deviceProviders = func() ([]devicekit.Provider, error) { return []devicekit.Provider{operatorDeviceFixture{}}, nil }
	listed := operatorTestRequest(server, "secret", http.MethodGet, "/v1/devices", "")
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(`"adapterId":"fixture"`)) {
		t.Fatalf("device inventory = %d %s", listed.Code, listed.Body.String())
	}
	denied := operatorTestRequest(server, "secret", http.MethodPost, "/v1/device-streams", `{"open":{"adapterId":"fixture","deviceId":"drive","grant":{"capabilities":["drive.read"],"resourceIds":["photos"]}},"action":{"capability":"drive.read","resourceId":"other","input":{}}}`)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("ungranted device resource = %d %s", denied.Code, denied.Body.String())
	}
	streamed := operatorTestRequest(server, "secret", http.MethodPost, "/v1/device-streams", `{"open":{"adapterId":"fixture","deviceId":"drive","grant":{"capabilities":["drive.read"],"resourceIds":["photos"]}},"action":{"capability":"drive.read","resourceId":"photos","input":{}}}`)
	if streamed.Code != http.StatusOK || streamed.Body.String() != "image" {
		t.Fatalf("device stream = %d %q", streamed.Code, streamed.Body.String())
	}
	invoked := operatorTestRequest(server, "secret", http.MethodPost, "/v1/device-actions", `{"open":{"adapterId":"fixture","deviceId":"drive","grant":{"capabilities":["drive.list"],"resourceIds":["photos"]}},"action":{"capability":"drive.list","resourceId":"photos","input":{}}}`)
	if invoked.Code != http.StatusOK || !bytes.Contains(invoked.Body.Bytes(), []byte("image.jpg")) {
		t.Fatalf("device action = %d %s", invoked.Code, invoked.Body.String())
	}
	httpServer := httptest.NewServer(server.Routes())
	defer httpServer.Close()
	t.Setenv("CYMONKEY_OPERATOR_TOKEN", "secret")
	var output bytes.Buffer
	if err := operatorDeviceClient([]string{"stream", "--url", httpServer.URL, "--adapter", "fixture", "--device", "drive", "--capability", "drive.read", "--resource", "photos"}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "image" {
		t.Fatalf("device CLI stream = %q", output.String())
	}
}
