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

type request struct {
	ID     uint64 `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}
type response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
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
	cmd := exec.Command(path)
	cmd.WaitDelay = 2 * time.Second
	cmd.Dir = installed.Directory
	// Do not inherit arbitrary host credentials. Plugins receive only explicit
	// request material and enough environment to launch on the current OS.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if runtime.GOOS == "windows" {
		cmd.Env = append(cmd.Env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
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
		APIVersion string `json:"apiVersion"`
		Name       string `json:"name"`
		Kind       string `json:"kind"`
	}
	helloCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.Call(helloCtx, "plugin.hello", nil, &hello); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("plugin handshake: %w", err)
	}
	if hello.APIVersion != APIVersion || hello.Name != installed.Manifest.Name || hello.Kind != installed.Manifest.Kind {
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
		if r.Error != "" {
			completed <- errors.New("plugin: " + r.Error)
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
