package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// StdioTransport attaches to caller-supplied process pipes. It owns the pipe
// attachment, never the process. Cancellation closes the pipes to unblock I/O.
// The host must not use these same pipes concurrently through another client.
type StdioTransport struct {
	mu       sync.Mutex
	input    io.ReadCloser
	output   io.WriteCloser
	reader   *bufio.Reader
	nextID   uint64
	once     sync.Once
	closeErr error
}

func NewStdio(input io.ReadCloser, output io.WriteCloser) (*StdioTransport, error) {
	if input == nil || output == nil {
		return nil, errors.New("MCP input and output pipes are required")
	}
	return &StdioTransport{input: input, output: output, reader: bufio.NewReaderSize(input, 4096)}, nil
}
func (t *StdioTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = t.Close() })
	defer stop()
	t.nextID++
	id := t.nextID
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	if err := t.write(payload); err != nil {
		return nil, err
	}
	for count := 0; count < 1024; count++ {
		var line []byte
		for {
			fragment, prefix, err := t.reader.ReadLine()
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, err
			}
			line = append(line, fragment...)
			if len(line) > 16<<20 {
				return nil, errors.New("MCP response exceeds 16 MiB")
			}
			if !prefix {
				break
			}
		}
		var message rpcReply
		if err := json.Unmarshal(line, &message); err != nil {
			return nil, err
		}
		if len(message.ID) == 0 || message.Method != "" {
			continue
		}
		return replyResult(line, id)
	}
	return nil, errors.New("too many MCP notifications without a reply")
}
func (t *StdioTransport) Notify(ctx context.Context, method string, params any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = t.Close() })
	defer stop()
	payload := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		payload["params"] = params
	}
	return t.write(payload)
}
func (t *StdioTransport) write(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	for len(data) > 0 {
		n, err := t.output.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
func (t *StdioTransport) Close() error {
	t.once.Do(func() { t.closeErr = errors.Join(t.input.Close(), t.output.Close()) })
	return t.closeErr
}
