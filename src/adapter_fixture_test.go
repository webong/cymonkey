package main

import (
	"github.com/webong/ext/pkg/plugin-go/contract/host/adapter"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The fake adapter answers management requests with canned files kept beside
// it: KIND.ACTION.out is the response, KIND.ACTION.err is written to stderr,
// and the request it received is saved as KIND.ACTION.seen.
const fakeAdapterScript = `#!/bin/sh
# usage: share PROFILE -- management KIND ACTION
dir=$(dirname "$0"); log="$FAKE_ADAPTER_LOG"; profile="$2"; kind="$5"; action="$6"
if [ "$4" = cookie ]; then kind=cookie; action="$5"; fi
cat > "$log/$kind.$action.seen"
printf '%s' "$profile" > "$log/$kind.$action.profile"
[ -f "$dir/$kind.$action.err" ] && cat "$dir/$kind.$action.err" >&2
if [ -f "$dir/$kind.$action.out" ]; then cat "$dir/$kind.$action.out"; exit 0; fi
echo "no canned response for $kind.$action" >&2; exit 1
`

type fakeAdapter struct {
	t    *testing.T
	home string
	name string
}

// installFakeAdapter installs and trusts a fake browser adapter that declares
// the given management operations. responses maps "kind.action" to the
// complete response JSON the adapter prints.
func installFakeAdapter(t *testing.T, name string, operations []string, responses map[string]string, stderr map[string]string) fakeAdapter {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake adapter is a POSIX script")
	}
	home := os.Getenv("CYMONKEY_ADAPTER_HOME")
	if home == "" {
		home = t.TempDir()
		t.Setenv("CYMONKEY_ADAPTER_HOME", home)
	}
	// Requests are logged outside the adapter directory, whose contents the
	// trust record covers. The adapters inherit this variable.
	log := os.Getenv("FAKE_ADAPTER_LOG")
	if log == "" {
		log = t.TempDir()
		t.Setenv("FAKE_ADAPTER_LOG", log)
	}
	source := t.TempDir()
	manifest := strings.Join([]string{
		`api_version = "2.0"`, `name = "` + name + `"`, `display_name = "` + name + `"`, `runtime = "browser"`, `surfaces = "web"`,
		`executable = "ctx-fake"`, `description = "test adapter"`, `capabilities = "list,validate,open,doctor,share"`,
		`selector_key = "browser"`, `share_spaces = "browser"`,
		`browser_management = "` + strings.Join(operations, ",") + `"`, `browser_share = "cookie.query,cookie.normalize"`,
	}, "\n") + "\n"
	write := func(file, content string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(source, file), []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("ctx-fake", fakeAdapterScript, 0o700)
	if err := adapter.WriteKeyValueManifest(source, manifest); err != nil {
		t.Fatal(err)
	}
	for key, response := range responses {
		write(key+".out", response+"\n", 0o600)
	}
	for key, text := range stderr {
		write(key+".err", text, 0o600)
	}
	host := browserHost()
	installed, err := host.Store().Install(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Store().Trust(installed); err != nil {
		t.Fatal(err)
	}
	return fakeAdapter{t: t, home: log, name: name}
}

// seen returns the request the adapter received for KIND.ACTION.
func (a fakeAdapter) seen(operation string) string {
	a.t.Helper()
	data, err := os.ReadFile(filepath.Join(a.home, operation+".seen"))
	if err != nil {
		a.t.Fatalf("adapter %s never received %s: %v", a.name, operation, err)
	}
	return string(data)
}

// profile returns the profile selector the adapter was invoked with.
func (a fakeAdapter) profile(operation string) string {
	a.t.Helper()
	data, err := os.ReadFile(filepath.Join(a.home, operation+".profile"))
	if err != nil {
		a.t.Fatal(err)
	}
	return string(data)
}

func managementResponse(kind, action, status, result string) string {
	body := `{"version":"1.0","kind":"` + kind + `","action":"` + action + `","status":"` + status + `"`
	if result != "" {
		body += `,"result":` + result
	}
	return body + `}`
}
