// Package serve is the guest side of a command service: it runs commands the
// host forwards, returns stdout whole, and streams stderr to the host as
// progress events. A package main calls Main with the command runner.
package serve

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

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"github.com/webong/ext/pkg/plugin-go/contract/transport/jsonline"
	"jangolova/commandkit"
)

// MaxCommandDuration is the guest-owned bound on one command. A command can run
// for a long time (a model install, a serve loop); the host bounds each call it
// makes as well, and a long call needs both raised.
const MaxCommandDuration = 7 * 24 * time.Hour

// Runner runs one command. stdin carries the request's input, which is empty
// when the host sent none.
type Runner func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error

// Main serves the contract name@version with runner. Started by a host it speaks
// the plugin protocol. Started with arguments instead it runs the command
// directly with the process's own input and output, which is how the host runs
// commands that speak a stdio protocol of their own.
func Main(name, version string, runner Runner) {
	if len(os.Args) > 1 {
		if err := runner(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := Run(context.Background(), name, version, runner); err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		os.Exit(1)
	}
}

// Run serves the contract until the host closes the session.
func Run(ctx context.Context, name, version string, runner Runner) error {
	descriptor, err := packagekit.LoadDescriptor(filepath.Join(filepath.Dir(os.Args[0]), store.ManifestName))
	if err != nil {
		return err
	}
	if !(packagekit.Manifest{Descriptor: descriptor}).Supports(name, version) {
		return fmt.Errorf("plugin manifest does not declare the %s contract", name)
	}
	return jsonline.ServeFunc(ctx, descriptor, Handler(name, runner), jsonline.FuncOptions{MaxCallDuration: MaxCommandDuration})
}

// Handler serves the command operation of contract name with runner.
func Handler(name string, runner Runner) jsonline.Func {
	return func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method != name+"."+commandkit.Operation {
			return nil, fmt.Errorf("unsupported method %q", method)
		}
		var request commandkit.Request
		if len(raw) == 0 || json.Unmarshal(raw, &request) != nil || len(request.Stdin) > commandkit.MaxStdinBytes {
			return nil, errors.New("decode command")
		}
		// What a command prints to stdout is its result and must arrive whole, so
		// it is returned with the call. What it logs to stderr streams to the
		// host as it happens; if no event can flow, it is returned instead.
		var stdout bounded
		logs := newLogStream(plugin.ProgressFrom(ctx).Report, "err")
		err := runner(ctx, request.Args, bytes.NewReader(request.Stdin), &stdout, logs)
		logs.flush()
		result := commandkit.Result{Stdout: stdout.buffer.Bytes(), Stderr: logs.returned(), Truncated: stdout.over || logs.over}
		if err != nil {
			result.Error = err.Error()
		}
		return result, nil
	}
}

// bounded keeps at most commandkit.MaxOutputBytes and remembers that it dropped more.
type bounded struct {
	buffer bytes.Buffer
	over   bool
}

func (b *bounded) Write(data []byte) (int, error) {
	written := len(data)
	room := commandkit.MaxOutputBytes - b.buffer.Len()
	if len(data) > room {
		b.over = true
		data = data[:max(room, 0)]
	}
	b.buffer.Write(data)
	return written, nil
}
