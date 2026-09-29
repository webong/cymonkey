package main

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

	"blockade"
)

type workerConfig struct {
	Command []string
	Workers int
	Env     []string
	Stderr  io.Writer
}

type workerPool struct {
	workers []*localWorker
	next    atomic.Uint64
	closed  atomic.Bool
}

type localWorker struct {
	config workerConfig
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
	ID           string                 `json:"id"`
	OK           bool                   `json:"ok"`
	Error        string                 `json:"error,omitempty"`
	APIVersion   string                 `json:"apiVersion,omitempty"`
	Observations []blockade.Observation `json:"observations,omitempty"`
}

func newWorkerPool(ctx context.Context, config workerConfig) (*workerPool, error) {
	if len(config.Command) == 0 || config.Command[0] == "" {
		return nil, errors.New("worker command is required")
	}
	if config.Workers <= 0 {
		config.Workers = 1
	}
	pool := &workerPool{}
	for index := range config.Workers {
		worker := &localWorker{config: config}
		if err := worker.start(ctx); err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("start worker %d: %w", index, err)
		}
		if _, err := worker.call(ctx, workerRequest{ID: fmt.Sprintf("health-%d", index), Method: "health"}); err != nil {
			worker.mu.Lock()
			worker.stopLocked()
			worker.mu.Unlock()
			_ = pool.Close()
			return nil, fmt.Errorf("check worker %d: %w", index, err)
		}
		pool.workers = append(pool.workers, worker)
	}
	return pool, nil
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
		return workerResponse{}, fmt.Errorf("write worker request: %w", err)
	}
	line, err := w.stdout.ReadBytes('\n')
	if err != nil {
		w.stopLocked()
		return workerResponse{}, fmt.Errorf("read worker response: %w", err)
	}
	var response workerResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return workerResponse{}, fmt.Errorf("decode worker response: %w", err)
	}
	if response.ID != request.ID {
		return workerResponse{}, fmt.Errorf("worker response id %q does not match request %q", response.ID, request.ID)
	}
	if !response.OK {
		return response, errors.New(response.Error)
	}
	return response, nil
}

func (p *workerPool) Observe(ctx context.Context, request blockade.ObserveRequest) (blockade.ObserveResponse, error) {
	if p == nil || len(p.workers) == 0 || p.closed.Load() {
		return blockade.ObserveResponse{}, errors.New("worker pool is closed")
	}
	if request.APIVersion == "" {
		request.APIVersion = blockade.APIVersion
	}
	if request.RequestID == "" {
		request.RequestID = fmt.Sprintf("blockade-ultralytics-%d", time.Now().UnixNano())
	}
	index := int(p.next.Add(1)-1) % len(p.workers)
	response, err := p.workers[index].call(ctx, workerRequest{
		ID: request.RequestID, Method: "observe", APIVersion: request.APIVersion,
		Image: request.Image, Prompt: request.Prompt,
	})
	if err != nil {
		return blockade.ObserveResponse{}, err
	}
	if response.APIVersion != blockade.APIVersion {
		return blockade.ObserveResponse{}, fmt.Errorf("unsupported worker apiVersion %q", response.APIVersion)
	}
	return blockade.ObserveResponse{
		APIVersion: response.APIVersion, RequestID: response.ID, Observations: response.Observations,
	}, nil
}

func (p *workerPool) Close() error {
	if p == nil || p.closed.Swap(true) {
		return nil
	}
	for _, worker := range p.workers {
		worker.mu.Lock()
		worker.stopLocked()
		worker.mu.Unlock()
	}
	return nil
}
