// Package browsermanage is Cymonkey's host side of ext's browser management
// protocol. Browser-specific work lives in trusted ext adapters. The host
// discovers an installed adapter, checks its trust record, runs it as
// "<adapter> share <profile> -- management <kind> <action>" with the request on
// stdin, and validates the response. It names no browser.
package browsermanage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/webong/ext/pkg/plugin-go/contract/host/adapter"
	"github.com/webong/ext/res/web/contract"
)

const (
	progressPrefix   = "browser management progress: "
	maxResponseBytes = 16<<20 + 4096
	maxStderrBytes   = 64 << 10
	shareOperation   = "share"
)

// Host runs trusted browser adapters from one adapter store.
type Host struct{ store *adapter.Store }

// New opens the adapter store at home. An empty home selects ext's shared
// adapter directory, so an adapter trusted once serves every ext consumer.
func New(home string) *Host {
	if home == "" {
		home = adapter.Home()
	}
	return &Host{store: adapter.NewStore(home)}
}

// Store exposes the adapter store for install and trust commands.
func (h *Host) Store() *adapter.Store { return h.store }

// Endpoint is one adapter and the profile selector it understands.
type Endpoint struct {
	Adapter *adapter.Adapter
	Profile string
}

// Adapters lists installed adapters that declare a management operation, for
// example "extension.install". An empty operation matches any declaration.
// Trust is checked when an adapter runs, not when it is listed.
func (h *Host) Adapters(operation string) ([]*adapter.Adapter, error) {
	all, err := h.store.List()
	if err != nil {
		return nil, err
	}
	var matches []*adapter.Adapter
	for _, candidate := range all {
		if operation == "" && len(candidate.Manifest.BrowserManagement) > 0 || operation != "" && candidate.HasBrowserManagement(operation) {
			matches = append(matches, candidate)
		}
	}
	return matches, nil
}

// Endpoint resolves "adapter" or "adapter:profile". The profile text is passed
// to the adapter unchanged because selectors are adapter-defined.
func (h *Host) Endpoint(selector string) (Endpoint, error) {
	name, profile, _ := strings.Cut(strings.TrimSpace(selector), ":")
	if name == "" {
		return Endpoint{}, errors.New("browser selection must name an adapter, as ADAPTER or ADAPTER:PROFILE")
	}
	loaded, err := h.store.Load(name)
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{Adapter: loaded, Profile: profile}, nil
}

// Progress receives one JSON progress value an adapter reported while running.
type Progress func(json.RawMessage) error

// Do runs one management operation to completion. progress may be nil.
func (h *Host) Do(ctx context.Context, endpoint Endpoint, kind, action string, input any, progress Progress) (contract.Response, error) {
	request, err := contract.NewRequest(kind, action, input)
	if err != nil {
		return contract.Response{}, err
	}
	operation := kind + "." + action
	if endpoint.Adapter == nil || !endpoint.Adapter.HasBrowserManagement(operation) {
		return contract.Response{}, fmt.Errorf("browser adapter does not support management operation %s", operation)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return contract.Response{}, err
	}
	output, err := h.run(ctx, endpoint, []string{"management", kind, action}, encoded, maxResponseBytes, false, progress)
	if err != nil {
		return contract.Response{}, err
	}
	var response contract.Response
	decoder := json.NewDecoder(bytes.NewReader(output))
	if err := decoder.Decode(&response); err != nil {
		return contract.Response{}, fmt.Errorf("browser adapter returned an invalid management response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return contract.Response{}, errors.New("browser adapter response contains trailing data")
	}
	if err := contract.ValidateResponse(response, request); err != nil {
		return contract.Response{}, err
	}
	return response, nil
}

// run starts a trusted adapter's share operation with arguments after "--",
// feeds it input on stdin, and returns what it wrote to stdout. A failure
// reports the adapter's diagnostic text, unless quiet is set for an operation
// that handles secrets, and never the request or the output.
func (h *Host) run(ctx context.Context, endpoint Endpoint, arguments []string, input []byte, limit int, quiet bool, progress Progress) ([]byte, error) {
	if err := h.store.AssertTrusted(endpoint.Adapter); err != nil {
		name := endpoint.Adapter.Manifest.Name
		return nil, fmt.Errorf("browser adapter %s is not trusted or has changed; review it, then run: cymy adapters trust %s", name, name)
	}
	command, err := endpoint.Adapter.CommandContext(ctx, adapter.Invocation{
		Operation: shareOperation, Selection: endpoint.Profile, Arguments: arguments,
	})
	if err != nil {
		return nil, err
	}
	command.Stdin = bytes.NewReader(input)
	stdout := &limitedBuffer{limit: limit}
	command.Stdout = stdout
	stderrReader, stderrWriter := io.Pipe()
	command.Stderr = stderrWriter
	diagnostics := &limitedBuffer{limit: maxStderrBytes}
	done := make(chan error, 1)
	go func() { done <- scanProgress(stderrReader, diagnostics, progress) }()
	if err := command.Start(); err != nil {
		_ = stderrWriter.Close()
		<-done
		return nil, err
	}
	runErr := command.Wait()
	_ = stderrWriter.Close()
	progressErr := <-done
	if runErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if quiet {
			// Diagnostics from an operation that handles secrets could echo them.
			return nil, fmt.Errorf("browser adapter %s failed: %w (its diagnostics are not shown)", endpoint.Adapter.Manifest.Name, runErr)
		}
		return nil, adapterError(endpoint.Adapter.Manifest.Name, runErr, diagnostics.String())
	}
	if progressErr != nil {
		return nil, progressErr
	}
	if stdout.truncated {
		return nil, errors.New("browser adapter response exceeds its size limit")
	}
	return stdout.Bytes(), nil
}

// Result decodes a response's result into out. A nil out ignores it.
func Result(response contract.Response, out any) error {
	if out == nil || len(response.Result) == 0 {
		return nil
	}
	return json.Unmarshal(response.Result, out)
}

// Installed reports whether the response proves a persistent installation.
// Only the "installed" status counts. Prepared, staged, signed,
// awaiting-browser-action and session-only "activated" results never do.
func Installed(response contract.Response) bool { return response.Status == "installed" }

func scanProgress(reader io.Reader, diagnostics *limitedBuffer, progress Progress) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	var failure error
	for scanner.Scan() {
		line := scanner.Text()
		if payload, ok := strings.CutPrefix(line, progressPrefix); ok && json.Valid([]byte(payload)) {
			if progress != nil && failure == nil {
				failure = progress(json.RawMessage(payload))
			}
			continue
		}
		diagnostics.WriteString(line + "\n")
	}
	if err := scanner.Err(); err != nil && failure == nil {
		failure = err
	}
	// Drain so the adapter is never blocked on a full pipe after a bad line.
	_, _ = io.Copy(io.Discard, reader)
	return failure
}

func adapterError(name string, runErr error, stderr string) error {
	message := strings.TrimSpace(stderr)
	if message == "" {
		return fmt.Errorf("browser adapter %s failed: %w", name, runErr)
	}
	return fmt.Errorf("browser adapter %s failed: %s", name, message)
}

// limitedBuffer keeps at most limit bytes and records truncation.
type limitedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	room := b.limit - b.Len()
	if room < len(p) {
		b.truncated = true
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func (b *limitedBuffer) WriteString(s string) (int, error) { return b.Write([]byte(s)) }
