package devices

import (
	"devicekit"
	"board/displayaction"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
)

type commandRunner struct{ env []string }

func (r commandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), r.env...)
	out := &boundedBuffer{remaining: 16 << 20}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		return nil, errors.New("device command failed")
	}
	return out.Bytes(), nil
}
func (r commandRunner) Start(name string, args ...string) (displayaction.RunningCommand, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), r.env...)
	if err := cmd.Start(); err != nil {
		return nil, errors.New("device process unavailable")
	}
	return &runningCommand{cmd: cmd}, nil
}

type runningCommand struct{ cmd *exec.Cmd }

func (r *runningCommand) Signal(s os.Signal) error { return r.cmd.Process.Signal(s) }
func (r *runningCommand) Kill() error              { return r.cmd.Process.Kill() }
func (r *runningCommand) Wait() error              { return r.cmd.Wait() }

type boundedBuffer struct {
	bytes.Buffer
	remaining int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.remaining {
		return 0, devicekit.ErrLimit
	}
	b.remaining -= len(p)
	return b.Buffer.Write(p)
}

type processChannel struct {
	reader io.ReadCloser
	writer io.WriteCloser
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
}

func processStream(ctx context.Context, env []string, read bool, name string, args ...string) (io.ReadWriteCloser, error) {
	life, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(life, name, args...)
	cmd.Env = append(os.Environ(), env...)
	p := &processChannel{cancel: cancel, done: make(chan error, 1)}
	var err error
	if read {
		p.reader, err = cmd.StdoutPipe()
	} else {
		p.writer, err = cmd.StdinPipe()
	}
	if err != nil {
		cancel()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		cancel()
		if p.reader != nil {
			_ = p.reader.Close()
		}
		if p.writer != nil {
			_ = p.writer.Close()
		}
		return nil, errors.New("device stream process unavailable")
	}
	go func() { p.done <- cmd.Wait() }()
	return p, nil
}
func (p *processChannel) Read(b []byte) (int, error) {
	if p.reader == nil {
		return 0, devicekit.ErrNotGranted
	}
	return p.reader.Read(b)
}
func (p *processChannel) Write(b []byte) (int, error) {
	if p.writer == nil {
		return 0, devicekit.ErrNotGranted
	}
	return p.writer.Write(b)
}
func (p *processChannel) Close() error {
	p.once.Do(func() {
		p.cancel()
		if p.reader != nil {
			_ = p.reader.Close()
		}
		if p.writer != nil {
			_ = p.writer.Close()
		}
		<-p.done
	})
	return nil
}
