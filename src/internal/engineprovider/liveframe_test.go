package engineprovider

import (
	"encoding/json"
	"testing"

	"cymonkey/pkg/liveframe"
)

func TestLiveframeConnectRequestMatchesDisplayContract(t *testing.T) {
	t.Parallel()
	session := liveframe.Session{
		WorkspaceDir: t.TempDir(),
		PageURL:      "http://127.0.0.1:4011/",
		CDPURL:       "ws://127.0.0.1:4012/devtools/browser/fixture",
		ServerPID:    31,
		BrowserPID:   32,
	}
	request, err := session.CymonkeyRequest("liveframe-fixture")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ConnectRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := validateConnectRequest(decoded); err != nil {
		t.Fatalf("Liveframe request rejected by Cymonkey: %v", err)
	}
	if decoded.Engine.Adapter != "web-presentation" || decoded.Target.Kind != "browser" || len(decoded.Engine.Approval.RequiredActions) != 3 {
		t.Fatalf("Liveframe request lost adapter, target, or approval policy: %+v", decoded)
	}
}
