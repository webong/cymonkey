// Package run is the host side of a command service: it forwards a command to
// the installed package that serves a contract, prints the log lines that
// stream back while it runs, and writes what the command returned.
package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host"
	"github.com/webong/ext/pkg/plugin-go/contract/host/process"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	"jangolova/commandkit"
)

// Timeout bounds one command. Model installs download large files and a serve
// loop runs until it is stopped, so the bound is long; canceling the context
// ends a command at any time.
const Timeout = 7 * 24 * time.Hour

// Policy admits the one operation a command service serves.
func Policy(_ context.Context, request plugin.Request) error {
	if request.Operation != commandkit.Operation {
		return fmt.Errorf("command service operation %q is not allowed", request.Operation)
	}
	return nil
}

// Command runs a command in the package. The package starts with ext's scrubbed
// environment plus env, so a command sees only the variables the host chose to
// give it. Canceling ctx stops the command and is not reported as a failure.
func Command(ctx context.Context, installed store.Installed, contract string, args []string, stdin []byte, env []string, stdout, stderr io.Writer) error {
	executable, err := installed.Executable()
	if err != nil {
		return err
	}
	proc, err := host.Open(ctx, installed, host.Options{
		Launch:    &process.Command{Path: executable, Env: env},
		Authorize: Policy,
		Timeout:   Timeout,
	})
	if err != nil {
		return err
	}
	defer proc.Close()
	var result commandkit.Result
	err = proc.CallProgress(ctx, contract+"."+commandkit.Operation, commandkit.Request{Args: args, Stdin: stdin}, &result, LogPrinter(stderr))
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	if _, err := stdout.Write(result.Stdout); err != nil {
		return err
	}
	if _, err := stderr.Write(result.Stderr); err != nil {
		return err
	}
	if result.Truncated {
		fmt.Fprintln(stderr, "command output was truncated")
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	return nil
}

// Exec runs the package's verified executable as a child with the process's own
// input and output, for commands that speak a stdio protocol of their own (an MCP
// server, a live event stream) and cannot be a single call. It gets the same
// scrubbed environment as a call.
func Exec(ctx context.Context, installed store.Installed, args []string, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := installed.Verify(); err != nil {
		return err
	}
	executable, err := installed.Executable()
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = installed.Directory
	command.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
	command.Stdin, command.Stdout, command.Stderr = stdin, stdout, stderr
	if err := command.Run(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// LogPrinter prints a command's streamed log lines as they arrive. A line the
// rate limit dropped is reported as a count, so a gap is never silent.
func LogPrinter(stderr io.Writer) func(plugin.Progress) {
	return func(event plugin.Progress) {
		if event.Dropped > 0 {
			fmt.Fprintf(stderr, "[%d log lines were dropped]\n", event.Dropped)
		}
		if event.Kind == "log" && (event.Text != "" || event.Dropped == 0) {
			fmt.Fprintln(stderr, event.Text)
		}
	}
}
