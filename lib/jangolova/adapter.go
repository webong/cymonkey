// Package jangolova implements Jangolova's caller-owned target integrations.
package jangolova

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

	"jangolova/sdk"
)

const defaultWorkerPath = "scripts/cymonkey-worker.mjs"

// Adapter attaches Jangolova to caller-owned targets. The portable Cymonkey
// registry and composition APIs live in Cymonkey's internal core.
type Adapter struct {
	Host     sdk.Host
	Backends []Backend
}

type instance struct {
	host           sdk.Host
	worker         sdk.Worker
	nodePath       string
	workerPath     string
	targetProtocol string
	driver         string
	policy         PolicyLimits
	endpoint       sdk.TargetEndpoint
	capabilities   []string

	callMu        sync.Mutex
	closed        bool
	disconnecting bool
	events        chan sdk.EngineEvent
	eventsMu      sync.RWMutex
	eventsClosed  bool
	eventsOnce    sync.Once
	renewalStop   chan struct{}
	renewalOnce   sync.Once
	renewalWG     sync.WaitGroup
}

var _ sdk.EngineAdapter = Adapter{}
var _ sdk.EngineInspector = Adapter{}
var _ sdk.EngineInstance = (*instance)(nil)
var _ sdk.EngineHealthProvider = (*instance)(nil)
var _ sdk.EngineCapabilityProvider = (*instance)(nil)
var _ sdk.EngineEventSource = (*instance)(nil)
var _ sdk.Caller = (*instance)(nil)

func (Adapter) InspectEngine(context.Context) sdk.EngineInspection {
	capabilities := stableStrings(append(capabilityNames(),
		"app.command.describe", "app.command.invoke", "app.command.list",
		"target.macos-cooperative", "target.windows-cooperative", "ui.action.invoke", "ui.attribute.set", "ui.query",
	))
	if _, err := exec.LookPath("node"); err != nil {
		return sdk.EngineInspection{Available: true, Capabilities: capabilities, Message: "macOS viewer runtime is available; browser runtime requires Node.js: " + err.Error()}
	}
	if _, err := resolveWorker(""); err != nil {
		return sdk.EngineInspection{Available: true, Capabilities: capabilities, Message: "macOS viewer runtime is available; browser runtime is unavailable: " + err.Error()}
	}
	return sdk.EngineInspection{Available: true, Capabilities: capabilities}
}

func (backend processBackend) Connect(ctx context.Context, spec sdk.EngineSpec, target sdk.EngineTarget, config Options) (sdk.EngineInstance, error) {
	if target.Kind != "browser" {
		return nil, errors.New("cymonkey requires target.kind browser")
	}
	endpoint, ok := target.Endpoint(backend.endpointProtocol)
	if !ok {
		return nil, fmt.Errorf("cymonkey %s backend requires a caller-owned %s endpoint", backend.name, backend.endpointProtocol)
	}
	if err := validateEndpoint(endpoint.URL, backend.endpointProtocol); err != nil {
		return nil, err
	}
	if err := config.Host.Validate(endpoint); err != nil {
		return nil, err
	}
	var err error
	nodePath := strings.TrimSpace(config.NodePath)
	if nodePath == "" {
		nodePath, err = exec.LookPath("node")
		if err != nil {
			return nil, fmt.Errorf("find Node.js for Cymonkey: %w", err)
		}
	}
	workerPath, err := resolveWorker(config.WorkerPath)
	if err != nil {
		return nil, err
	}
	running := &instance{
		host:     config.Host,
		nodePath: nodePath, workerPath: workerPath, targetProtocol: backend.endpointProtocol, driver: config.Driver,
		policy:   config.Policy,
		endpoint: endpoint, events: make(chan sdk.EngineEvent, 8), renewalStop: make(chan struct{}),
	}
	snapshot := endpoint.Snapshot()
	worker, capabilities, err := running.startWorker(ctx)
	if err != nil {
		return nil, err
	}
	running.worker = worker
	running.capabilities = capabilities
	if missing := missingCapabilities(spec.RequiredCapabilities, capabilities); len(missing) != 0 {
		worker.Terminate()
		return nil, fmt.Errorf("Cymonkey backend %s is missing required capabilities: %s", backend.name, strings.Join(missing, ", "))
	}
	if source := strings.TrimSpace(spec.Source); source != "" {
		params, _ := json.Marshal(map[string]any{
			"name":  "window.navigate",
			"input": map[string]string{"url": source},
		})
		if _, err := running.Call(ctx, sdk.MethodAct, params); err != nil {
			_ = running.Disconnect(context.Background())
			return nil, fmt.Errorf("navigate Cymonkey target: %w", err)
		}
	}
	go running.monitorWorker(worker)
	if endpoint.Connection != nil {
		updates := endpoint.Connection.Updates()
		running.renewalWG.Add(1)
		go func() {
			defer running.renewalWG.Done()
			running.watchConnectionMaterial(updates, snapshot)
		}()
	}
	running.emit(sdk.EngineEvent{Type: "cymonkey.connected", Status: sdk.EngineHealthHealthy, OccurredAt: time.Now().UTC()})
	return running, nil
}

func (i *instance) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case sdk.MethodHello, sdk.MethodCapabilities, sdk.MethodDescribe, sdk.MethodAct, sdk.MethodEvents, "health":
	default:
		return nil, fmt.Errorf("unsupported Cymonkey interaction method %q", method)
	}
	if method == sdk.MethodAct {
		action, err := decodeAction(params)
		if err != nil {
			return nil, fmt.Errorf("decode Cymonkey action: %w", err)
		}
		if !capabilityAllowed(i.policy.AllowedCapabilities, action.Name) {
			return nil, fmt.Errorf("Cymonkey policy denied capability %q", action.Name)
		}
		if rawURL, _ := action.Input["url"].(string); !originAllowed(i.policy.AllowedOrigins, rawURL) {
			return nil, fmt.Errorf("Cymonkey policy denied origin %q", rawURL)
		}
	}
	return i.request(ctx, method, params)
}

func (i *instance) request(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	i.callMu.Lock()
	defer i.callMu.Unlock()
	if i.closed || i.worker == nil {
		return nil, errors.New("Cymonkey worker is disconnected")
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
	i.finish(sdk.EngineEvent{Type: "cymonkey.disconnected", Status: sdk.EngineHealthStopped, OccurredAt: time.Now().UTC()})
	return err
}

func (i *instance) EngineHealth(ctx context.Context) sdk.EngineHealth {
	health := sdk.EngineHealth{ObservedAt: time.Now().UTC()}
	if err := i.host.Validate(i.endpoint); err != nil {
		health.Status, health.Message = sdk.EngineHealthUnhealthy, err.Error()
		return health
	}
	result, err := i.request(ctx, "health", json.RawMessage(`{}`))
	if err != nil {
		health.Status, health.Message = sdk.EngineHealthUnhealthy, err.Error()
		return health
	}
	var value struct {
		Connected bool `json:"connected"`
	}
	if err := json.Unmarshal(result, &value); err != nil || !value.Connected {
		health.Status, health.Message = sdk.EngineHealthUnhealthy, "Cymonkey browser target is disconnected"
		return health
	}
	health.Status = sdk.EngineHealthHealthy
	return health
}

func (i *instance) Authorize(ctx context.Context, request sdk.AuthorizeRequest) (sdk.AuthorizeDecision, error) {
	action := strings.TrimSpace(request.Action)
	if action == "" {
		return sdk.AuthorizeDecision{Authorized: false}, errors.New("Cymonkey interaction action name is required")
	}
	if !capabilityAllowed(i.policy.AllowedCapabilities, action) {
		return sdk.AuthorizeDecision{Authorized: false, Reason: fmt.Sprintf("Cymonkey policy denied capability %q", action)}, nil
	}
	return sdk.AuthorizeDecision{Authorized: true}, nil
}

func (i *instance) EngineCapabilities() []string {
	i.callMu.Lock()
	defer i.callMu.Unlock()
	return append([]string(nil), i.capabilities...)
}

func (i *instance) EngineEvents() <-chan sdk.EngineEvent { return i.events }

func (i *instance) startWorker(ctx context.Context) (sdk.Worker, []string, error) {
	environment, err := i.host.NodeEnvironment(i.endpoint, os.Environ())
	if err != nil {
		return nil, nil, err
	}
	if i.host.StartWorker == nil {
		return nil, nil, errors.New("Jangolova host worker startup is required")
	}
	worker, err := i.host.StartWorker(i.nodePath, i.workerPath, nil, environment)
	if err != nil {
		return nil, nil, fmt.Errorf("start Cymonkey worker: %w", err)
	}
	snapshot := i.endpoint.Snapshot()
	params, _ := json.Marshal(map[string]any{
		"endpoint": i.endpoint.URL, "protocol": i.targetProtocol, "driver": i.driver,
		"headers": snapshot.Headers, "policy": i.policy,
	})
	result, err := worker.Call(ctx, "connect", params)
	if err != nil {
		worker.Terminate()
		return nil, nil, fmt.Errorf("connect Cymonkey to target: %w%s", err, worker.StderrSuffix())
	}
	var connected struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(result, &connected); err != nil {
		worker.Terminate()
		return nil, nil, fmt.Errorf("decode Cymonkey worker handshake: %w", err)
	}
	return worker, stableStrings(append(capabilityNames(), connected.Capabilities...)), nil
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
		return errors.New("Cymonkey worker is disconnected")
	}
	previous := i.worker
	i.worker, i.capabilities = candidate, capabilities
	i.callMu.Unlock()
	go i.monitorWorker(candidate)
	if previous != nil {
		drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = previous.Disconnect(drainCtx)
		cancel()
	}
	return nil
}

func (i *instance) watchConnectionMaterial(updates <-chan uint64, connected sdk.EndpointConnectionSnapshot) {
	for {
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
			current := i.endpoint.Snapshot()
			var err error
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			if current.TLSRevision > connected.TLSRevision {
				err = i.replaceWorker(ctx)
			} else {
				params, _ := json.Marshal(map[string]any{
					"endpoint": i.endpoint.URL, "protocol": i.targetProtocol,
					"headers": current.Headers, "policy": i.policy,
				})
				_, err = i.request(ctx, "reconnect", params)
			}
			cancel()
			if err != nil {
				i.emit(sdk.EngineEvent{Type: "cymonkey.connection.renewal_failed", Message: i.host.RedactString(err.Error(), sdk.EngineTarget{Endpoints: []sdk.TargetEndpoint{i.endpoint}}), OccurredAt: time.Now().UTC()})
				continue
			}
			i.endpoint.Connection.Acknowledge(current.Revision)
			connected = current
			i.emit(sdk.EngineEvent{Type: "cymonkey.connection.renewed", OccurredAt: time.Now().UTC()})
		}
	}
}

func (i *instance) monitorWorker(worker sdk.Worker) {
	<-worker.Done()
	i.callMu.Lock()
	if i.worker != worker || i.disconnecting || i.closed {
		i.callMu.Unlock()
		return
	}
	i.closed, i.worker = true, nil
	i.callMu.Unlock()
	i.stopConnectionMaterialWatch()
	message := "Cymonkey worker exited" + worker.StderrSuffix()
	if err := worker.WaitError(); err != nil {
		message = err.Error() + worker.StderrSuffix()
	}
	i.finish(sdk.EngineEvent{Type: "cymonkey.failed", Status: sdk.EngineHealthUnhealthy, Message: message, OccurredAt: time.Now().UTC()})
}

func (i *instance) emit(event sdk.EngineEvent) {
	i.eventsMu.RLock()
	defer i.eventsMu.RUnlock()
	if i.eventsClosed {
		return
	}
	select {
	case i.events <- event:
	default:
	}
}

func (i *instance) finish(event sdk.EngineEvent) {
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

func (i *instance) stopConnectionMaterialWatch() { i.renewalOnce.Do(func() { close(i.renewalStop) }) }

func decodeOptions(raw json.RawMessage) (Options, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		value := Options{}
		_ = normalizeOptions(&value)
		return value, nil
	}
	var value Options
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Options{}, fmt.Errorf("decode Cymonkey options: %w", err)
	}
	if err := normalizeOptions(&value); err != nil {
		return Options{}, err
	}
	return value, nil
}

func validateEndpoint(value, protocol string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parse Cymonkey CDP endpoint: %w", err)
	}
	if protocol == "webdriver-bidi" && parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return errors.New("Cymonkey WebDriver BiDi endpoint must use ws or wss")
	}
	if protocol == "cdp" {
		switch parsed.Scheme {
		case "http", "https", "ws", "wss":
		default:
			return fmt.Errorf("Cymonkey CDP endpoint has unsupported scheme %q", parsed.Scheme)
		}
	}
	if parsed.Host == "" {
		return errors.New("Cymonkey CDP endpoint must include a host")
	}
	return nil
}

func resolveWorker(configured string) (string, error) {
	candidates := []string{strings.TrimSpace(configured), strings.TrimSpace(os.Getenv("JANGOLOVA_CYMONKEY_WORKER")), defaultWorkerPath, "/usr/local/lib/jangolova/cymonkey-worker.mjs"}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "..", "lib", "jangolova", "cymonkey-worker.mjs"))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if absolute, err := filepath.Abs(candidate); err == nil {
			candidate = absolute
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("Cymonkey worker not found; set JANGOLOVA_CYMONKEY_WORKER")
}

func capabilityNames() []string {
	return []string{
		"act", "augmentation", "augmentation.install", "augmentation.update", "augmentation.uninstall",
		"augmentation.enable", "augmentation.disable", "augmentation.list", "augmentation.describe",
		"window.click", "window.evaluate", "window.fill", "window.navigate", "window.press", "window.screenshot",
		"capabilities", "describe", "events", "document.observe", "document.patch", "document.query", "network.observe",
		"network.rules.install", "network.rules.remove",
		"overlay.mount", "overlay.patch", "overlay.unmount", "script.execute", "script.register",
		"script.unregister", "storage.get", "storage.set", "style.insert", "style.remove",
		"target.cdp", "target.safari-mcp", "target.webdriver-bidi",
	}
}

func missingCapabilities(required, actual []string) []string {
	available := make(map[string]struct{}, len(actual))
	for _, value := range actual {
		available[value] = struct{}{}
	}
	missing := make([]string, 0)
	for _, value := range required {
		if _, ok := available[value]; !ok {
			missing = append(missing, value)
		}
	}
	return stableStrings(missing)
}

func stableStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
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
