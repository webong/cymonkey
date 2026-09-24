package cymonkeyhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const shutdownTimeout = 10 * time.Second

// Supervisor starts and monitors the standalone component processes declared
// by a Cymonkey host config. If one component exits, the host stops the rest
// and reports the failure to the operator.
type Supervisor struct {
	config Config
	stdout io.Writer
	stderr io.Writer

	mu        sync.Mutex
	processes []*managedProcess
}

type managedProcess struct {
	component ComponentConfig
	command   *exec.Cmd
	done      chan error
}

func NewSupervisor(config Config, stdout, stderr io.Writer) (*Supervisor, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return &Supervisor{config: config, stdout: stdout, stderr: stderr}, nil
}

// Run blocks until the host context is cancelled or a component exits. A
// caller-requested cancellation is a clean shutdown and returns nil.
func (s *Supervisor) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("Cymonkey host context is required")
	}
	events := make(chan processEvent, len(s.config.Components))
	for _, component := range s.config.Components {
		process, err := s.start(component)
		if err != nil {
			s.stopAll()
			return fmt.Errorf("start Cymonkey component %q: %w", component.ID, err)
		}
		s.mu.Lock()
		s.processes = append(s.processes, process)
		s.mu.Unlock()
		go func(process *managedProcess) {
			err := process.command.Wait()
			process.done <- err
			events <- processEvent{component: process.component, err: err}
		}(process)
	}
	select {
	case <-ctx.Done():
		s.stopAll()
		return nil
	case event := <-events:
		s.stopAll()
		if event.err == nil {
			return fmt.Errorf("Cymonkey component %q exited unexpectedly", event.component.ID)
		}
		return fmt.Errorf("Cymonkey component %q exited: %w", event.component.ID, event.err)
	}
}

type processEvent struct {
	component ComponentConfig
	err       error
}

func (s *Supervisor) start(component ComponentConfig) (*managedProcess, error) {
	command := exec.Command(component.Command[0], component.Command[1:]...)
	command.Env = append(os.Environ(), environment(component.Environment)...)
	command.Dir = component.WorkingDirectory
	command.Stdout = s.stdout
	command.Stderr = s.stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	return &managedProcess{component: component, command: command, done: make(chan error, 1)}, nil
}

func environment(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+os.ExpandEnv(value))
	}
	return result
}

func (s *Supervisor) stopAll() {
	s.mu.Lock()
	processes := append([]*managedProcess(nil), s.processes...)
	s.mu.Unlock()
	for _, process := range processes {
		if process.command.Process != nil {
			_ = process.command.Process.Signal(os.Interrupt)
		}
	}
	deadline := time.NewTimer(shutdownTimeout)
	defer deadline.Stop()
	for _, process := range processes {
		select {
		case <-process.done:
		case <-deadline.C:
			for _, remaining := range processes {
				if remaining.command.Process != nil {
					_ = remaining.command.Process.Kill()
				}
			}
			return
		}
	}
}
