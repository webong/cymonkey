// Package browserautomation implements Playwright and Puppeteer interaction
// engines that attach to caller-owned CDP or WebDriver BiDi browser targets.
package browserautomation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cymonkey/src/internal/bridge"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/nodeworker"
	"cymonkey/src/internal/orchestrator"
	"cymonkey/src/targetconn"
)

const defaultWorkerPath = "scripts/browser-worker.mjs"

type Adapter struct {
	implementation string
}

func Playwright() Adapter { return Adapter{implementation: "playwright"} }
func Puppeteer() Adapter  { return Adapter{implementation: "puppeteer"} }

type options struct {
	NodePath   string `json:"nodePath,omitempty"`
	WorkerPath string `json:"workerPath,omitempty"`
}

type instance struct {
	implementation string
	worker         *nodeworker.Process
	nodePath       string
	workerPath     string
	events         chan orchestrator.EngineEvent
	eventsMu       sync.RWMutex
	eventsClosed   bool
	capabilities   []string
	callMu         sync.Mutex
	disconnecting  bool
	closed         bool
	eventsOnce     sync.Once
	endpoint       orchestrator.TargetEndpoint
	targetProtocol string
	renewalStop    chan struct{}
	renewalOnce    sync.Once
	renewalWG      sync.WaitGroup
}

var _ orchestrator.EngineAdapter = Adapter{}
var _ orchestrator.EngineInspector = Adapter{}
var _ orchestrator.EngineInstance = (*instance)(nil)
var _ orchestrator.EngineHealthProvider = (*instance)(nil)
var _ orchestrator.EngineCapabilityProvider = (*instance)(nil)
var _ orchestrator.EngineEventSource = (*instance)(nil)
var _ bridge.Caller = (*instance)(nil)

func (a Adapter) InspectEngine(context.Context) orchestrator.EngineInspection {
	capabilities := capabilityNames(a.implementation)
	if _, err := exec.LookPath("node"); err != nil {
		return orchestrator.EngineInspection{Capabilities: capabilities, Message: "Node.js is required: " + err.Error()}
	}
	if _, err := resolveWorker(""); err != nil {
		return orchestrator.EngineInspection{Capabilities: capabilities, Message: err.Error()}
	}
	return orchestrator.EngineInspection{Available: true, Capabilities: capabilities}
}

func (a Adapter) Connect(
	ctx context.Context,
	spec manifest.EngineSpec,
	target orchestrator.EngineTarget,
) (orchestrator.EngineInstance, error) {
	if target.Kind != "browser" {
		return nil, fmt.Errorf("%s requires target.kind browser", a.implementation)
	}
	endpoint, targetProtocol, ok := a.targetEndpoint(target)
	if !ok {
		return nil, fmt.Errorf("%s requires a compatible caller-owned browser endpoint", a.implementation)
	}
	if err := validateEndpoint(endpoint.URL, targetProtocol); err != nil {
		return nil, err
	}
	if err := targetconn.Validate(endpoint); err != nil {
		return nil, err
	}
	if targetProtocol == "webdriver-bidi" && !strings.HasPrefix(endpoint.URL, "ws://") && !strings.HasPrefix(endpoint.URL, "wss://") {
		return nil, errors.New("WebDriver BiDi target endpoint must use ws or wss")
	}
	config, err := decodeOptions(spec.Options)
	if err != nil {
		return nil, err
	}
	nodePath := strings.TrimSpace(config.NodePath)
	if nodePath == "" {
		nodePath, err = exec.LookPath("node")
		if err != nil {
			return nil, fmt.Errorf("find Node.js for %s: %w", a.implementation, err)
		}
	}
	workerPath, err := resolveWorker(config.WorkerPath)
	if err != nil {
		return nil, err
	}
	running := &instance{
		implementation: a.implementation,
		nodePath:       nodePath,
		workerPath:     workerPath,
		events:         make(chan orchestrator.EngineEvent, 8),
		endpoint:       endpoint,
		targetProtocol: targetProtocol,
		renewalStop:    make(chan struct{}),
	}
	connectionSnapshot := endpoint.Connection.Snapshot()
	worker, capabilities, err := running.startWorker(ctx)
	if err != nil {
		return nil, err
	}
	running.worker = worker
	running.capabilities = capabilities
	go running.monitorWorker(worker)
	if endpoint.Connection != nil {
		updates := endpoint.Connection.Updates()
		running.renewalWG.Add(1)
		go func() {
			defer running.renewalWG.Done()
			running.watchConnectionMaterial(updates, connectionSnapshot)
		}()
	}
	if source := strings.TrimSpace(spec.Source); source != "" {
		params, _ := json.Marshal(map[string]any{
			"name":  "window.navigate",
			"input": map[string]string{"url": source},
		})
		if _, err := running.Call(ctx, bridge.MethodAct, params); err != nil {
			_ = running.Disconnect(context.Background())
			return nil, fmt.Errorf("navigate %s target: %w", a.implementation, err)
		}
	}
	return running, nil
}

func (a Adapter) targetEndpoint(target orchestrator.EngineTarget) (orchestrator.TargetEndpoint, string, bool) {
	if endpoint, ok := target.Endpoint("cdp"); ok {
		return endpoint, "cdp", true
	}
	if a.implementation == "puppeteer" {
		if endpoint, ok := target.Endpoint("webdriver-bidi"); ok {
			return endpoint, "webdriver-bidi", true
		}
	}
	return orchestrator.TargetEndpoint{}, "", false
}

func (i *instance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case bridge.MethodHello, bridge.MethodCapabilities, bridge.MethodDescribe, bridge.MethodAct, bridge.MethodEvents:
	default:
		return nil, fmt.Errorf("unsupported interaction method %q", method)
	}
	return i.request(ctx, method, params)
}

func (i *instance) request(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	i.callMu.Lock()
	defer i.callMu.Unlock()
	if i.closed || i.worker == nil {
		return nil, errors.New("interaction worker is disconnected")
	}
	return i.worker.Call(ctx, method, params)
}

func (i *instance) Disconnect(ctx context.Context) error {
	i.stopConnectionMaterialWatch()
	i.renewalWG.Wait()
	i.callMu.Lock()
	if i.closed {
		i.callMu.Unlock()
		return nil
	}
	i.disconnecting = true
	worker := i.worker
	i.worker = nil
	i.closed = true
	i.callMu.Unlock()
	err := worker.Disconnect(ctx)
	i.finish(orchestrator.EngineEvent{Type: "interaction.disconnected", Status: "disconnected", OccurredAt: time.Now().UTC()})
	return err
}

func (i *instance) EngineHealth(ctx context.Context) orchestrator.EngineHealth {
	health := orchestrator.EngineHealth{ObservedAt: time.Now().UTC()}
	if err := targetconn.Validate(i.endpoint); err != nil {
		health.Status = orchestrator.EngineHealthUnhealthy
		health.Message = err.Error()
		return health
	}
	result, err := i.request(ctx, "health", json.RawMessage(`{}`))
	if err != nil {
		health.Status = orchestrator.EngineHealthUnhealthy
		health.Message = err.Error()
		return health
	}
	var value struct {
		Connected bool `json:"connected"`
	}
	if err := json.Unmarshal(result, &value); err != nil || !value.Connected {
		health.Status = orchestrator.EngineHealthUnhealthy
		health.Message = "browser interaction target is disconnected"
		return health
	}
	health.Status = orchestrator.EngineHealthHealthy
	return health
}

func (i *instance) Authorize(ctx context.Context, request orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	if strings.TrimSpace(request.Action) == "" {
		return orchestrator.AuthorizeDecision{Authorized: false}, errors.New("browser interaction action name is required")
	}
	return orchestrator.AuthorizeDecision{Authorized: true}, nil
}

func (i *instance) EngineCapabilities() []string {
	i.callMu.Lock()
	defer i.callMu.Unlock()
	return append([]string(nil), i.capabilities...)
}

func (i *instance) EngineEvents() <-chan orchestrator.EngineEvent { return i.events }

func (i *instance) watchConnectionMaterial(updates <-chan uint64, connected orchestrator.EndpointConnectionSnapshot) {
	for {
		current := i.endpoint.Connection.Snapshot()
		if current.Revision > connected.Revision {
			if !i.applyConnectionMaterial(current, connected) {
				return
			}
			connected = current
			continue
		}
		select {
		case <-i.renewalStop:
			return
		case revision, open := <-updates:
			if !open {
				return
			}
			if revision <= connected.Revision {
				continue
			}
			current = i.endpoint.Connection.Snapshot()
			if !i.applyConnectionMaterial(current, connected) {
				return
			}
			connected = current
		}
	}
}

func (i *instance) applyConnectionMaterial(current, connected orchestrator.EndpointConnectionSnapshot) bool {
	reportedFailure := false
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		var err error
		if current.TLSRevision > connected.TLSRevision {
			err = i.replaceWorker(ctx)
		} else {
			params, _ := json.Marshal(map[string]any{
				"endpoint": i.endpoint.URL, "protocol": i.targetProtocol, "headers": current.Headers,
			})
			_, err = i.request(ctx, "reconnect", params)
		}
		cancel()
		if err == nil {
			i.endpoint.Connection.Acknowledge(current.Revision)
			return i.emitConnectionEvent(orchestrator.EngineEvent{Type: "interaction.connection.renewed", OccurredAt: time.Now().UTC()})
		}
		if !reportedFailure {
			reportedFailure = true
			if !i.emitConnectionEvent(orchestrator.EngineEvent{
				Type:       "interaction.connection.renewal_failed",
				Message:    targetconn.RedactString(err.Error(), orchestrator.EngineTarget{Endpoints: []orchestrator.TargetEndpoint{i.endpoint}}),
				OccurredAt: time.Now().UTC(),
			}) {
				return false
			}
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-timer.C:
		case <-i.renewalStop:
			timer.Stop()
			return false
		}
	}
}

func (i *instance) emitConnectionEvent(event orchestrator.EngineEvent) bool {
	i.eventsMu.RLock()
	defer i.eventsMu.RUnlock()
	if i.eventsClosed {
		return false
	}
	select {
	case i.events <- event:
		return true
	case <-i.renewalStop:
		return false
	}
}

func (i *instance) stopConnectionMaterialWatch() {
	i.renewalOnce.Do(func() { close(i.renewalStop) })
}

func (i *instance) startWorker(ctx context.Context) (*nodeworker.Process, []string, error) {
	environment, err := targetconn.NodeEnvironment(i.endpoint, os.Environ())
	if err != nil {
		return nil, nil, err
	}
	worker, err := nodeworker.Start(i.nodePath, i.workerPath, []string{"--adapter", i.implementation}, environment)
	if err != nil {
		return nil, nil, fmt.Errorf("start %s interaction worker: %w", i.implementation, err)
	}
	snapshot := i.endpoint.Connection.Snapshot()
	params, _ := json.Marshal(map[string]any{
		"endpoint": i.endpoint.URL, "protocol": i.targetProtocol, "headers": snapshot.Headers,
	})
	result, err := worker.Call(ctx, "connect", params)
	if err != nil {
		worker.Terminate()
		return nil, nil, fmt.Errorf("connect %s to target: %w%s", i.implementation, err, worker.StderrSuffix())
	}
	var connected struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(result, &connected); err != nil {
		worker.Terminate()
		return nil, nil, fmt.Errorf("decode %s worker handshake: %w", i.implementation, err)
	}
	return worker, stableStrings(connected.Capabilities), nil
}

func (i *instance) replaceWorker(ctx context.Context) error {
	candidate, capabilities, err := i.startWorker(ctx)
	if err != nil {
		return err
	}
	i.callMu.Lock()
	if i.closed || i.disconnecting {
		i.callMu.Unlock()
		candidate.Terminate()
		return errors.New("interaction worker is disconnected")
	}
	previous := i.worker
	i.worker = candidate
	i.capabilities = capabilities
	i.callMu.Unlock()
	go i.monitorWorker(candidate)
	if previous != nil {
		drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = previous.Disconnect(drainCtx)
		cancel()
	}
	return nil
}

func (i *instance) monitorWorker(worker *nodeworker.Process) {
	<-worker.Done()
	i.callMu.Lock()
	if i.worker != worker || i.disconnecting || i.closed {
		i.callMu.Unlock()
		return
	}
	i.closed = true
	i.worker = nil
	i.callMu.Unlock()
	i.stopConnectionMaterialWatch()
	message := "interaction worker exited" + worker.StderrSuffix()
	if err := worker.WaitError(); err != nil {
		message = err.Error() + worker.StderrSuffix()
	}
	i.finish(orchestrator.EngineEvent{Type: "interaction.failed", Status: "failed", Message: message, OccurredAt: time.Now().UTC()})
}

func (i *instance) finish(event orchestrator.EngineEvent) {
	i.eventsOnce.Do(func() {
		i.eventsMu.Lock()
		defer i.eventsMu.Unlock()
		if i.eventsClosed {
			return
		}
		select {
		case i.events <- event:
		default:
		}
		i.eventsClosed = true
		close(i.events)
	})
}

func decodeOptions(raw json.RawMessage) (options, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return options{}, nil
	}
	var value options
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return options{}, fmt.Errorf("decode browser automation options: %w", err)
	}
	return value, nil
}

func validateEndpoint(value, protocol string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parse %s target endpoint: %w", protocol, err)
	}
	switch parsed.Scheme {
	case "http", "https", "ws", "wss":
	default:
		return fmt.Errorf("%s target endpoint has unsupported scheme %q", protocol, parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s target endpoint must include a host", protocol)
	}
	return nil
}

func resolveWorker(configured string) (string, error) {
	candidates := []string{strings.TrimSpace(configured), strings.TrimSpace(os.Getenv("JANGOLOVA_BROWSER_WORKER")), defaultWorkerPath, "/usr/local/lib/jangolova/browser-worker.mjs"}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "..", "lib", "jangolova", "browser-worker.mjs"))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err == nil {
			candidate = absolute
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("browser interaction worker not found; set JANGOLOVA_BROWSER_WORKER")
}

func capabilityNames(implementation string) []string {
	capabilities := []string{"act", "window.click", "window.evaluate", "window.fill", "window.navigate", "window.press", "window.screenshot", "capabilities", "describe", "events", "target.cdp"}
	if implementation == "puppeteer" {
		capabilities = append(capabilities, "target.webdriver-bidi")
	}
	return capabilities
}

func stableStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
