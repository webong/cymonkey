// content serves the content engine as an ext package that speaks the
// content.service contract. The host sends a public content command; the engine
// runs it, hosting the content providers it needs. Started with arguments
// instead of by a host, it runs the command directly with the process's own
// input and output, which is how long-running commands such as serve run.
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

	"console/contentcmd"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"github.com/webong/ext/pkg/plugin-go/contract/transport/jsonline"
	"jangolova/contentkit/service"
)

func main() {
	if len(os.Args) > 1 {
		if err := contentcmd.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
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
	return jsonline.ServeFunc(ctx, descriptor, call, jsonline.FuncOptions{})
}

func call(_ context.Context, method string, raw json.RawMessage) (any, error) {
	if method != service.ContractName+".command" {
		return nil, fmt.Errorf("unsupported method %q", method)
	}
	var request service.CommandRequest
	if len(raw) == 0 || json.Unmarshal(raw, &request) != nil {
		return nil, errors.New("decode content command")
	}
	var stdout, stderr bounded
	err := contentcmd.Run(request.Args, &stdout, &stderr)
	result := service.CommandResult{Stdout: stdout.buffer.Bytes(), Stderr: stderr.buffer.Bytes(), Truncated: stdout.over || stderr.over}
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
