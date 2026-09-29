package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"board"
	"cymonkey/src/internal/orchestrator"
)

type operatorBoardFixture struct{}

func (operatorBoardFixture) ID() string { return "fixture" }
func (operatorBoardFixture) List(context.Context) ([]board.Device, error) {
	return []board.Device{{ID: "drive", Kind: "drive", Capabilities: []board.CapabilityDescriptor{
		{Name: board.DriveList, InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: board.DriveRead, InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}}, nil
}
func (operatorBoardFixture) Open(context.Context, string, board.Grant) (board.Session, error) {
	return operatorBoardSession{}, nil
}

type operatorBoardSession struct{}

func (operatorBoardSession) Invoke(context.Context, board.Action) (board.Result, error) {
	return board.Result{Output: json.RawMessage(`{"files":["image.jpg"]}`)}, nil
}
func (operatorBoardSession) Stream(context.Context, board.Action) (board.Content, error) {
	return &operatorBoardContent{Reader: bytes.NewReader([]byte("image"))}, nil
}
func (operatorBoardSession) Close(context.Context) error { return nil }

type operatorBoardContent struct{ *bytes.Reader }

func (*operatorBoardContent) Size() int64     { return 5 }
func (*operatorBoardContent) Truncated() bool { return false }
func (*operatorBoardContent) Close() error    { return nil }

func TestOperatorBoardUsesExplicitGrantForActionsAndStreams(t *testing.T) {
	server, err := newOperatorServer(orchestrator.NewRegistry(), "secret", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	server.boardProviders = func() ([]board.Provider, error) { return []board.Provider{operatorBoardFixture{}}, nil }
	listed := operatorTestRequest(server, "secret", http.MethodGet, "/v1/board/devices", "")
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(`"providerId":"fixture"`)) {
		t.Fatalf("Board devices = %d %s", listed.Code, listed.Body.String())
	}
	denied := operatorTestRequest(server, "secret", http.MethodPost, "/v1/board/streams", `{"open":{"providerId":"fixture","deviceId":"drive","grant":{"capabilities":["drive.read"],"resourceIds":["photos"]}},"action":{"capability":"drive.read","resourceId":"other","input":{}}}`)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("ungranted Board resource = %d %s", denied.Code, denied.Body.String())
	}
	streamed := operatorTestRequest(server, "secret", http.MethodPost, "/v1/board/streams", `{"open":{"providerId":"fixture","deviceId":"drive","grant":{"capabilities":["drive.read"],"resourceIds":["photos"]}},"action":{"capability":"drive.read","resourceId":"photos","input":{}}}`)
	if streamed.Code != http.StatusOK || streamed.Body.String() != "image" {
		t.Fatalf("Board stream = %d %q", streamed.Code, streamed.Body.String())
	}
	invoked := operatorTestRequest(server, "secret", http.MethodPost, "/v1/board/actions", `{"open":{"providerId":"fixture","deviceId":"drive","grant":{"capabilities":["drive.list"],"resourceIds":["photos"]}},"action":{"capability":"drive.list","resourceId":"photos","input":{}}}`)
	if invoked.Code != http.StatusOK || !bytes.Contains(invoked.Body.Bytes(), []byte("image.jpg")) {
		t.Fatalf("Board action = %d %s", invoked.Code, invoked.Body.String())
	}
	httpServer := httptest.NewServer(server.Routes())
	defer httpServer.Close()
	t.Setenv("CYMONKEY_OPERATOR_TOKEN", "secret")
	var output bytes.Buffer
	if err := operatorBoardClient([]string{"stream", "--url", httpServer.URL, "--provider", "fixture", "--device", "drive", "--capability", "drive.read", "--resource", "photos"}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "image" {
		t.Fatalf("Board CLI stream = %q", output.String())
	}
}
