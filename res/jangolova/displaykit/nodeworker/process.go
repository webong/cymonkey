// Package nodeworker manages a line-delimited JSON RPC Node.js subprocess.
// Its Process is intentionally target-agnostic so adapters can replace an
// entire worker when process-scoped security material changes.
package nodeworker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

type request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type Process struct {
	command   *exec.Cmd
	stdin     io.WriteCloser
	responses chan response
	done      chan struct{}
	stderr    lockedBuffer
	nextID    atomic.Uint64
	waitMu    sync.RWMutex
	waitErr   error
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(value)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.b.String())
}

func Start(nodePath, workerPath string, args, environment []string) (*Process, error) {
	command := exec.Command(nodePath, append([]string{workerPath}, args...)...)
	command.Env = environment
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, errors.New("open worker input")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, errors.New("open worker output")
	}
	process := &Process{
		command: command, stdin: stdin, responses: make(chan response, 1), done: make(chan struct{}),
	}
	command.Stderr = &process.stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start interaction worker: %w", err)
	}
	go process.readResponses(stdout)
	go process.wait()
	return process, nil
}

// Call sends one RPC request. A Process supports one caller at a time; the
// owning adapter serializes calls while it performs atomic worker swaps.
func (p *Process) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	id := p.nextID.Add(1)
	document, _ := json.Marshal(request{ID: id, Method: method, Params: params})
	if _, err := p.stdin.Write(append(document, '\n')); err != nil {
		return nil, fmt.Errorf("write worker request: %w", err)
	}
	select {
	case item, open := <-p.responses:
		if !open {
			return nil, errors.New("interaction worker exited")
		}
		if item.ID != id {
			return nil, fmt.Errorf("worker response id %d does not match request %d", item.ID, id)
		}
		if item.Error != "" {
			return nil, errors.New(item.Error)
		}
		if !json.Valid(item.Result) {
			return nil, errors.New("worker returned invalid JSON")
		}
		return item.Result, nil
	case <-ctx.Done():
		p.Terminate()
		return nil, ctx.Err()
	}
}

func (p *Process) Disconnect(ctx context.Context) error {
	_, requestErr := p.Call(ctx, "disconnect", json.RawMessage(`{}`))
	_ = p.stdin.Close()
	select {
	case <-p.done:
		if requestErr != nil {
			return requestErr
		}
		return p.WaitError()
	case <-ctx.Done():
		p.Terminate()
		return ctx.Err()
	}
}

func (p *Process) Done() <-chan struct{} { return p.done }

func (p *Process) WaitError() error {
	p.waitMu.RLock()
	defer p.waitMu.RUnlock()
	return p.waitErr
}

func (p *Process) Terminate() {
	if p.command.Process != nil {
		_ = p.command.Process.Kill()
	}
}

func (p *Process) StderrSuffix() string {
	if value := p.stderr.String(); value != "" {
		return ": " + value
	}
	return ""
}

func (p *Process) readResponses(stdout io.Reader) {
	defer close(p.responses)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var item response
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			item.Error = "decode worker response: " + err.Error()
		}
		p.responses <- item
	}
}

func (p *Process) wait() {
	err := p.command.Wait()
	p.waitMu.Lock()
	p.waitErr = err
	p.waitMu.Unlock()
	close(p.done)
}
