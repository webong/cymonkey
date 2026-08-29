package browserextensionprotocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRecordedControlCallsDecodeWithGeneratedBindings(t *testing.T) {
	for _, name := range []string{"extension-cymonkey-act", "policy-replace"} {
		contents := readFixture(t, name)
		var call ControlCall
		if err := json.Unmarshal(contents, &call); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		if call.Type != CallTypeJangolova {
			t.Fatalf("%s decoded non-standard call type %q", name, call.Type)
		}
		if call.Method == "" {
			t.Fatalf("%s decoded empty method", name)
		}
	}
}

func TestRecordedAuthenticationUsesCurrentProtocol(t *testing.T) {
	contents := readFixture(t, "websocket-auth")
	var request AuthRequest
	if err := json.Unmarshal(contents, &request); err != nil {
		t.Fatal(err)
	}
	if request.ProtocolVersion != ProtocolVersion {
		t.Fatalf("fixture protocol = %q, generated protocol = %q", request.ProtocolVersion, ProtocolVersion)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "protocol", "browser-extension", "v1alpha1", "fixtures", name+".json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}
