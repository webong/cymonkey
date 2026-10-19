// content serves the content engine as an ext package that speaks the
// content.service contract. The host sends a public content command; the engine
// runs it, hosting the content providers it needs. What the command prints to
// stdout is returned with the call; what it logs to stderr streams to the host as
// progress events while it runs, which is how long-running commands such as
// serve report.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"console/contentcmd"
	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"github.com/webong/ext/pkg/plugin-go/contract/transport/jsonline"
	"jangolova/contentkit/service"
)

// maxCommandDuration is the guest-owned bound on one command.
const maxCommandDuration = 7 * 24 * time.Hour

// maxKept bounds the stderr text returned with a result.
const maxKept = service.MaxOutputBytes

func main() {
	if err := serve(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "content:", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context) error {
	descriptor, err := packagekit.LoadDescriptor(filepath.Join(filepath.Dir(os.Args[0]), store.ManifestName))
	if err != nil {
		return err
	}
	if !(packagekit.Manifest{Descriptor: descriptor}).Supports(service.ContractName, service.ContractVersion) {
		return errors.New("plugin manifest does not declare the content.service contract")
	}
	// A command can run for a long time (a model install, the serve loop), so the
	// guest's own bound is generous; the host bounds each call it makes.
	return jsonline.ServeFunc(ctx, descriptor, call, jsonline.FuncOptions{MaxCallDuration: maxCommandDuration})
}

func call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if method != service.ContractName+".command" {
		return nil, fmt.Errorf("unsupported method %q", method)
	}
	var request service.CommandRequest
	if len(raw) == 0 || json.Unmarshal(raw, &request) != nil {
		return nil, errors.New("decode content command")
	}
	// What a command prints to stdout is its result and must arrive whole, so it
	// is returned with the call. What it logs to stderr streams to the host as it
	// happens; if the host cannot receive events, it is returned instead.
	var stdout bounded
	logs := newLogStream(plugin.ProgressFrom(ctx).Report, "err")
	err := contentcmd.RunContext(ctx, request.Args, &stdout, logs)
	logs.flush()
	result := service.CommandResult{Stdout: stdout.buffer.Bytes(), Stderr: logs.returned(), Truncated: stdout.over || logs.over}
	if err != nil {
		result.Error = err.Error()
	}
	return result, nil
}

// bounded keeps at most service.MaxOutputBytes and remembers that it dropped more.
type bounded struct {
	buffer bytes.Buffer
	over   bool
}

func (b *bounded) Write(data []byte) (int, error) {
	written := len(data)
	room := service.MaxOutputBytes - b.buffer.Len()
	if len(data) > room {
		b.over = true
		data = data[:max(room, 0)]
	}
	b.buffer.Write(data)
	return written, nil
}

var _ io.Writer = (*bounded)(nil)
