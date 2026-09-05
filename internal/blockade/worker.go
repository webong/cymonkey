package blockade

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type WorkerConfig struct {
	Command []string
	Workers int
	Env     []string
	Stderr  io.Writer
}

type WorkerPool struct {
	workers []*localWorker
	next    atomic.Uint64
	closed  atomic.Bool
}
type localWorker struct {
	config WorkerConfig
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}
type workerRequest struct {
	ID         string `json:"id"`
	Method     string `json:"method"`
	APIVersion string `json:"apiVersion"`
	Image      []byte `json:"image,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
}
type workerResponse struct {
	ID           string        `json:"id"`
	OK           bool          `json:"ok"`
	Error        string        `json:"error,omitempty"`
	APIVersion   string        `json:"apiVersion,omitempty"`
	Observations []Observation `json:"observations,omitempty"`
}

func NewWorkerPool(ctx context.Context, config WorkerConfig) (*WorkerPool, error) {
	if len(config.Command) == 0 || config.Command[0] == "" {
		return nil, errors.New("Blockade worker command is required")
	}
	if config.Workers <= 0 {
		config.Workers = 1
	}
	p := &WorkerPool{}
	for i := 0; i < config.Workers; i++ {
		w := &localWorker{config: config}
		if err := w.start(ctx); err != nil {
			_ = p.Close()
			return nil, fmt.Errorf("start Blockade worker %d: %w", i, err)
		}
		p.workers = append(p.workers, w)
	}
	return p, nil
}

func (w *localWorker) start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.startLocked(ctx)
}
func (w *localWorker) startLocked(ctx context.Context) error {
	if w.cmd != nil && w.cmd.Process != nil {
		return nil
	}
	w.cmd = exec.CommandContext(ctx, w.config.Command[0], w.config.Command[1:]...)
	w.cmd.Env = append(os.Environ(), w.config.Env...)
	if w.config.Stderr == nil {
		w.cmd.Stderr = os.Stderr
	} else {
		w.cmd.Stderr = w.config.Stderr
	}
	stdin, err := w.cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := w.cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	if err := w.cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}
	w.stdin, w.stdout = stdin, bufio.NewReaderSize(stdout, 16<<20)
	return nil
}
func (w *localWorker) stopLocked() {
	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_ = w.cmd.Wait()
	}
	w.cmd, w.stdin, w.stdout = nil, nil, nil
}
func (w *localWorker) call(ctx context.Context, request workerRequest) (workerResponse, error) {
	if err := ctx.Err(); err != nil {
		return workerResponse{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cmd == nil {
		if err := w.startLocked(ctx); err != nil {
			return workerResponse{}, err
		}
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return workerResponse{}, err
	}
	if _, err := w.stdin.Write(append(encoded, '\n')); err != nil {
		w.stopLocked()
		return workerResponse{}, fmt.Errorf("write Blockade worker request: %w", err)
	}
	line, err := w.stdout.ReadBytes('\n')
	if err != nil {
		w.stopLocked()
		return workerResponse{}, fmt.Errorf("read Blockade worker response: %w", err)
	}
	var response workerResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return workerResponse{}, fmt.Errorf("decode Blockade worker response: %w", err)
	}
	if response.ID != request.ID {
		return workerResponse{}, fmt.Errorf("Blockade worker response id %q does not match request %q", response.ID, request.ID)
	}
	if !response.OK {
		return response, errors.New(response.Error)
	}
	return response, nil
}
func (p *WorkerPool) Observe(ctx context.Context, request ObserveRequest) (ObserveResponse, error) {
	if p == nil || len(p.workers) == 0 || p.closed.Load() {
		return ObserveResponse{}, errors.New("Blockade worker pool is closed")
	}
	if request.APIVersion == "" {
		request.APIVersion = APIVersion
	}
	if request.RequestID == "" {
		request.RequestID = fmt.Sprintf("blockade-%d", time.Now().UnixNano())
	}
	i := int(p.next.Add(1)-1) % len(p.workers)
	response, err := p.workers[i].call(ctx, workerRequest{ID: request.RequestID, Method: "observe", APIVersion: request.APIVersion, Image: request.Image, Prompt: request.Prompt})
	if err != nil {
		return ObserveResponse{}, err
	}
	if response.APIVersion != APIVersion {
		return ObserveResponse{}, fmt.Errorf("unsupported Blockade worker apiVersion %q", response.APIVersion)
	}
	return ObserveResponse{APIVersion: response.APIVersion, RequestID: response.ID, Observations: response.Observations}, nil
}

func (p *WorkerPool) Capabilities(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil || len(p.workers) == 0 || p.closed.Load() {
		return nil, errors.New("Blockade worker pool is closed")
	}
	return []string{CapabilityImageObserve, CapabilityObjectDetect, CapabilityObjectSegment}, nil
}

func (p *WorkerPool) Health(ctx context.Context) (EngineHealth, error) {
	if err := ctx.Err(); err != nil {
		return EngineHealth{}, err
	}
	ready := p != nil && len(p.workers) != 0 && !p.closed.Load()
	return EngineHealth{Ready: ready}, nil
}

func (p *WorkerPool) Close() error {
	if p == nil || p.closed.Swap(true) {
		return nil
	}
	for _, w := range p.workers {
		w.mu.Lock()
		w.stopLocked()
		w.mu.Unlock()
	}
	return nil
}
