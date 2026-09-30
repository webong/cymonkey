package providerplugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

const maxFrame = 24 << 20
const maxRetryAfterMilliseconds = int64((365 * 24 * time.Hour) / time.Millisecond)

type request struct {
	ID     uint64 `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}
type response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

// RemoteError is a machine-readable failure returned by a provider process.
// Code is intentionally transport-agnostic; domain adapters decide which
// codes they recognize. Older providers may continue returning string errors.
type RemoteError struct {
	Code                   string `json:"code,omitempty"`
	Message                string `json:"message"`
	RetryAfterMilliseconds int64  `json:"retryAfterMilliseconds,omitempty"`
}

func (e *RemoteError) Error() string {
	if e == nil || e.Message == "" {
		return "plugin returned an error"
	}
	return "plugin: " + e.Message
}

// Process is one plugin process with serialized, bounded JSON-line calls.
// A canceled call terminates the process; no later call can reuse it.
type Process struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdoutPipe io.ReadCloser
	stdout     *bufio.Scanner
	mu         sync.Mutex
	next       uint64
	closed     bool
	closeOnce  sync.Once
	stderr     *boundedText
}

type boundedText struct {
	mu   sync.Mutex
	text []byte
}

func (b *boundedText) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.text) < 4096 {
		n := 4096 - len(b.text)
		if n > len(p) {
			n = len(p)
		}
		b.text = append(b.text, p[:n]...)
	}
	return len(p), nil
}
func (b *boundedText) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.text) }

// Open verifies the installed executable and requires its hello response to
// match the reviewed manifest before returning it to a domain adapter.
func Open(ctx context.Context, installed Installed) (*Process, error) {
	path, err := installed.executable()
	if err != nil {
		return nil, err
	}
	return OpenCommand(ctx, installed, Command{Executable: path})
}

// Command is a domain-owned launch decision for a verified source package.
// Executable may live outside the package, such as a Blockade-managed runtime.
type Command struct {
	Executable  string
	Arguments   []string
	Environment []string
}

// OpenCommand verifies the entire package before starting it with a runtime
// chosen by the domain library. The same manifest handshake is still required.
func OpenCommand(ctx context.Context, installed Installed, launch Command) (*Process, error) {
	if err := installed.Verify(); err != nil {
		return nil, err
	}
	if launch.Executable == "" {
		return nil, errors.New("plugin launch executable is required")
	}
	info, err := os.Lstat(launch.Executable)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("plugin launch executable must be a regular file")
	}
	cmd := exec.Command(launch.Executable, launch.Arguments...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Dir = installed.Directory
	// Do not inherit arbitrary host credentials. Plugins receive only explicit
	// request material and enough environment to launch on the current OS.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if runtime.GOOS == "windows" {
		cmd.Env = append(cmd.Env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	cmd.Env = append(cmd.Env, launch.Environment...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &boundedText{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), maxFrame)
	p := &Process{cmd: cmd, stdin: stdin, stdoutPipe: stdout, stdout: scanner, stderr: stderr}
	var hello struct {
		APIVersion string     `json:"apiVersion"`
		Name       string     `json:"name"`
		Version    string     `json:"version"`
		Contracts  []Contract `json:"contracts,omitempty"`
		Kind       string     `json:"kind,omitempty"`
	}
	helloCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.Call(helloCtx, "plugin.hello", nil, &hello); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("plugin handshake: %w", err)
	}
	if hello.APIVersion != installed.Manifest.APIVersion || hello.Name != installed.Manifest.Name || hello.Version != installed.Manifest.Version || !sameContractIdentity(hello.Kind, hello.Contracts, installed.Manifest.Kind, installed.Manifest.Contracts) {
		_ = p.Close()
		return nil, errors.New("plugin handshake does not match installed manifest")
	}
	return p, nil
}

func (p *Process) Call(ctx context.Context, method string, params any, result any) error {
	if p == nil {
		return errors.New("plugin process is nil")
	}
	if method == "" {
		return errors.New("plugin method is required")
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("plugin process is closed")
	}
	p.next++
	id := p.next
	frame, err := json.Marshal(request{ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	if len(frame) > maxFrame-1 {
		return errors.New("plugin request exceeds frame limit")
	}
	completed := make(chan error, 1)
	go func() {
		if _, err := p.stdin.Write(append(frame, '\n')); err != nil {
			completed <- err
			return
		}
		if !p.stdout.Scan() {
			if err := p.stdout.Err(); err != nil {
				completed <- err
			} else {
				completed <- io.EOF
			}
			return
		}
		var r response
		if err := json.Unmarshal(p.stdout.Bytes(), &r); err != nil {
			completed <- err
			return
		}
		if r.ID != id {
			completed <- errors.New("plugin response ID mismatch")
			return
		}
		if len(r.Error) != 0 && string(r.Error) != "null" {
			completed <- decodeRemoteError(r.Error)
			return
		}
		if result != nil {
			if err := json.Unmarshal(r.Result, result); err != nil {
				completed <- err
				return
			}
		}
		completed <- nil
	}()
	select {
	case err := <-completed:
		if err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		_ = p.stdin.Close()
		_ = p.stdoutPipe.Close()
		<-completed
		p.closed = true
		return ctx.Err()
	}
}

func decodeRemoteError(raw json.RawMessage) error {
	var legacy string
	if err := json.Unmarshal(raw, &legacy); err == nil {
		if legacy == "" {
			return errors.New("plugin returned an empty error")
		}
		return errors.New("plugin: " + legacy)
	}
	var remote RemoteError
	if err := json.Unmarshal(raw, &remote); err != nil {
		return errors.New("plugin returned a malformed error")
	}
	if remote.Message == "" || remote.RetryAfterMilliseconds < 0 || remote.RetryAfterMilliseconds > maxRetryAfterMilliseconds {
		return errors.New("plugin returned an invalid error")
	}
	return &remote
}

func (p *Process) Close() error {
	if p == nil {
		return nil
	}
	var result error
	p.closeOnce.Do(func() {
		_ = p.cmd.Process.Kill()
		_ = p.stdin.Close()
		_ = p.stdoutPipe.Close()
		_ = p.cmd.Wait()
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		result = nil
	})
	return result
}
