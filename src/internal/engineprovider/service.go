package engineprovider

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cymonkey/src/internal/bridge"
	"cymonkey/src/internal/manifest"
	"cymonkey/src/internal/orchestrator"
	"cymonkey/src/targetconn"
)

var instanceIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var handleNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)
var targetIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)

const eventHistoryLimit = 256
const defaultApprovalLifetime = 5 * time.Minute

const (
	defaultRecoveryInitialBackoff = 250 * time.Millisecond
	defaultRecoveryMaximumBackoff = 10 * time.Second
	defaultRecoveryConnectTimeout = 30 * time.Second
)

type Service struct {
	mu                     sync.Mutex
	registry               *orchestrator.Registry
	token                  string
	resolver               targetconn.Resolver
	instances              map[string]*runningInstance
	recoveryInitialBackoff time.Duration
	recoveryMaximumBackoff time.Duration
	recoveryConnectTimeout time.Duration
}

type runningInstance struct {
	request        ConnectRequest
	adapter        string
	status         string
	health         Health
	instance       orchestrator.EngineInstance
	engine         orchestrator.EngineAdapter
	spec           manifest.EngineSpec
	target         orchestrator.EngineTarget
	release        func(context.Context) error
	redact         func(string) string
	redactJSON     func(json.RawMessage) json.RawMessage
	recoveryCancel context.CancelFunc
	recoveryDone   chan struct{}
	healthFailures int
	events         []InstanceEvent
	nextEvent      uint64
	approvals      map[string]*pendingApproval
}

type pendingApproval struct {
	action   string
	digest   string
	status   string
	expires  time.Time
	consumed bool
}

type ServiceOption func(*Service)

func WithTargetResolver(resolver targetconn.Resolver) ServiceOption {
	return func(service *Service) { service.resolver = resolver }
}

func NewService(registry *orchestrator.Registry, token string, options ...ServiceOption) (*Service, error) {
	if registry == nil {
		return nil, errors.New("engine provider registry is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("engine provider token is required")
	}
	service := &Service{
		registry:               registry,
		token:                  token,
		resolver:               targetconn.DefaultResolver(),
		instances:              make(map[string]*runningInstance),
		recoveryInitialBackoff: defaultRecoveryInitialBackoff,
		recoveryMaximumBackoff: defaultRecoveryMaximumBackoff,
		recoveryConnectTimeout: defaultRecoveryConnectTimeout,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service, nil
}

func (s *Service) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/engines", s.handleEngines)
	mux.HandleFunc("/v1/instances", s.handleInstances)
	mux.HandleFunc("/v1/instances/", s.handleInstance)
	mux.HandleFunc("/v1/reconcile", s.handleReconcile)
	return s.authorize(mux)
}

func (s *Service) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		supplied := strings.TrimSpace(strings.TrimPrefix(header, prefix))
		if !strings.HasPrefix(header, prefix) ||
			len(supplied) != len(s.token) ||
			subtle.ConstantTimeCompare([]byte(supplied), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authorization is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"service":    "jangolova-interaction-provider",
		"apiVersion": APIVersion,
	})
}

func (s *Service) handleEngines(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/engines" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"apiVersion": APIVersion,
		"engines":    DiscoverEngines(r.Context(), s.registry),
	})
}

func (s *Service) handleInstances(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/instances" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var request ConnectRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid connection request")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "connection request must contain one JSON value")
		return
	}
	if err := validateConnectRequest(request); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", err.Error())
		return
	}
	adapterName := strings.TrimSpace(request.Engine.Adapter)
	if adapterName == "auto" {
		selected, selectErr := SelectAutomaticEngine(r.Context(), s.registry, request.Target, request.Engine.RequiredCapabilities)
		if selectErr != nil {
			writeError(w, http.StatusUnprocessableEntity, "engine_not_compatible", selectErr.Error())
			return
		}
		adapterName = selected
	}
	adapter, ok := s.registry.Engine(adapterName)
	if !ok {
		writeError(w, http.StatusNotFound, "engine_not_found", "interaction engine is not registered")
		return
	}

	record, err := s.connectAndRecord(r.Context(), request.InstanceID, adapterName, request, adapter)
	if err != nil {
		var failure *connectFailure
		if !errors.As(err, &failure) {
			writeError(w, http.StatusBadGateway, "engine_connect_failed", err.Error())
			return
		}
		switch failure.code {
		case "instance_exists":
			writeError(w, http.StatusConflict, "instance_exists", failure.message)
		case "target_resolution_failed":
			writeError(w, http.StatusBadGateway, "target_resolution_failed", failure.message)
		default:
			writeError(w, http.StatusBadGateway, "engine_connect_failed", failure.message)
		}
		return
	}
	s.mu.Lock()
	value := describeInstance(request.InstanceID, record)
	if launchProvider, ok := record.instance.(orchestrator.EngineCallerLaunchProvider); ok {
		launch := launchProvider.EngineCallerLaunch()
		value.CallerLaunch = &CallerLaunch{Environment: cloneValues(launch.Environment)}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, value)
}

func (s *Service) handleInstance(w http.ResponseWriter, r *http.Request) {
	relative := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/instances/"), "/")
	parts := strings.Split(relative, "/")
	id := parts[0]
	if !instanceIDPattern.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "events" {
		s.handleInstanceEvents(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "call" {
		s.handleInstanceCall(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "approvals" {
		s.handleApprovalRequest(w, r, id)
		return
	}
	if len(parts) == 3 && parts[1] == "approvals" {
		s.handleApprovalResolution(w, r, id, parts[2])
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	record, ok := s.instances[id]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "instance_not_found", "interaction instance was not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if record.status != "connected" {
			value := describeInstance(id, record)
			s.mu.Unlock()
			writeJSON(w, http.StatusOK, value)
			return
		}
		instance := record.instance
		s.mu.Unlock()
		health := probeInstanceHealth(r.Context(), instance)
		s.mu.Lock()
		current, exists := s.instances[id]
		if !exists || current != record {
			s.mu.Unlock()
			writeError(w, http.StatusNotFound, "instance_not_found", "interaction instance was not found")
			return
		}
		if record.redact != nil {
			health.Message = record.redact(health.Message)
		}
		updateInstanceHealth(record, health)
		if health.Status == orchestrator.EngineHealthUnhealthy {
			record.healthFailures++
			if record.healthFailures >= 2 {
				s.beginRecoveryLocked(id, record, instance, health.Message)
			}
		} else {
			record.healthFailures = 0
		}
		value := describeInstance(id, record)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, value)
	case http.MethodDelete:
		if record.status == "disconnecting" {
			s.mu.Unlock()
			writeError(w, http.StatusConflict, "instance_disconnecting", "interaction instance is disconnecting")
			return
		}
		record.status = "disconnecting"
		if record.recoveryCancel != nil {
			record.recoveryCancel()
			record.recoveryCancel = nil
		}
		recoveryDone := record.recoveryDone
		record.health = Health{
			Status:     orchestrator.EngineHealthStopping,
			Message:    "interaction engine is disconnecting",
			ObservedAt: time.Now().UTC(),
		}
		appendInstanceEvent(record, orchestrator.EngineEvent{
			Type:       "instance.disconnecting",
			Status:     "disconnecting",
			OccurredAt: time.Now().UTC(),
		})
		instance := record.instance
		s.mu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		var err error
		if recoveryDone != nil {
			select {
			case <-recoveryDone:
			case <-ctx.Done():
				err = errors.Join(err, errors.New("cancel interaction recovery: "+ctx.Err().Error()))
			}
		}
		if instance != nil {
			err = errors.Join(err, instance.Disconnect(ctx))
		}
		if record.redact != nil && err != nil {
			err = errors.New(record.redact(err.Error()))
		}
		if record.release != nil {
			err = errors.Join(err, record.release(ctx))
		}
		s.mu.Lock()
		delete(s.instances, id)
		s.mu.Unlock()
		if err != nil {
			writeError(w, http.StatusBadGateway, "engine_disconnect_failed", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.mu.Unlock()
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Service) handleInstanceEvents(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	after, err := parseCursor(r.URL.Query().Get("after"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_cursor", err.Error())
		return
	}
	limit, err := parseEventLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_limit", err.Error())
		return
	}

	s.mu.Lock()
	record, ok := s.instances[id]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "instance_not_found", "interaction instance was not found")
		return
	}
	if after > record.nextEvent {
		s.mu.Unlock()
		writeError(w, http.StatusUnprocessableEntity, "invalid_cursor", "event cursor is ahead of the instance")
		return
	}
	if len(record.events) != 0 {
		first, _ := strconv.ParseUint(record.events[0].Cursor, 10, 64)
		if after+1 < first {
			s.mu.Unlock()
			writeError(w, http.StatusGone, "cursor_expired", "event cursor is older than retained history")
			return
		}
	}
	events := make([]InstanceEvent, 0, limit)
	cursor := record.nextEvent
	for _, event := range record.events {
		sequence, _ := strconv.ParseUint(event.Cursor, 10, 64)
		if sequence <= after {
			continue
		}
		events = append(events, event)
		cursor = sequence
		if len(events) == limit {
			break
		}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, InstanceEventBatch{
		APIVersion: APIVersion,
		InstanceID: id,
		Events:     events,
		Cursor:     strconv.FormatUint(cursor, 10),
	})
}

func (s *Service) handleInstanceCall(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var request CallRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid call request")
		return
	}
	if strings.TrimSpace(request.Method) == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", "method is required")
		return
	}
	if len(request.Params) == 0 {
		request.Params = json.RawMessage(`{}`)
	}
	if !json.Valid(request.Params) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", "params must be valid JSON")
		return
	}

	s.mu.Lock()
	record, ok := s.instances[id]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "instance_not_found", "interaction instance was not found")
		return
	}
	if record.status != "connected" {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "instance_not_connected", "interaction instance is not connected")
		return
	}
	caller, ok := record.instance.(bridge.Caller)
	actionName := ""
	if request.Method == "act" {
		actionName = actionNameForAudit(request.Params)
		appendInstanceEvent(record, orchestrator.EngineEvent{
			Type:       "action.requested",
			Status:     "requested",
			Message:    actionName,
			OccurredAt: time.Now().UTC(),
		})
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotImplemented, "calls_unsupported", "interaction engine does not accept bridge calls")
		return
	}

	// Per-capability policy gate: authorize the action before dispatch.
	if request.Method == "act" {
		if err := authorizeAction(r.Context(), record.instance, request.Params); err != nil {
			s.appendActionAudit(id, record, "denied", actionName, err.Error())
			writeError(w, http.StatusForbidden, "policy_denied", err.Error())
			return
		}
		if requiresApproval(record.request.Engine.Approval, actionName) {
			if err := s.consumeApproval(id, record, request.ApprovalID, actionName, request.Params); err != nil {
				s.appendActionAudit(id, record, "denied", actionName, err.Error())
				writeError(w, http.StatusForbidden, "approval_required", err.Error())
				return
			}
		}
	}

	callCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := caller.Call(callCtx, request.Method, request.Params)
	if err != nil {
		if record.redact != nil {
			err = errors.New(record.redact(err.Error()))
		}
		if request.Method == "act" {
			s.appendActionAudit(id, record, "failed", actionName, err.Error())
		}
		writeError(w, http.StatusBadGateway, "engine_call_failed", err.Error())
		return
	}
	if record.redactJSON != nil {
		result = record.redactJSON(result)
	}
	if !json.Valid(result) {
		if request.Method == "act" {
			s.appendActionAudit(id, record, "failed", actionName, "interaction engine returned invalid JSON")
		}
		writeError(w, http.StatusBadGateway, "invalid_engine_result", "interaction engine returned invalid JSON")
		return
	}
	if request.Method == "act" {
		s.appendActionAudit(id, record, "completed", actionName, "")
	}
	writeJSON(w, http.StatusOK, CallResponse{
		APIVersion: APIVersion,
		InstanceID: id,
		Result:     result,
	})
}

func (s *Service) watchInstanceEvents(
	id string,
	record *runningInstance,
	instance orchestrator.EngineInstance,
	events <-chan orchestrator.EngineEvent,
) {
	for event := range events {
		s.mu.Lock()
		current, ok := s.instances[id]
		if !ok || current != record || record.instance != instance {
			s.mu.Unlock()
			return
		}
		if record.redact != nil {
			event.Message = record.redact(event.Message)
		}
		if record.status == "disconnecting" &&
			(event.Status == "exited" || event.Status == "failed") {
			event.Type = "instance.disconnected"
			event.Status = "disconnected"
			event.Message = ""
			record.status = "stopped"
			record.health = Health{
				Status:     orchestrator.EngineHealthStopped,
				ObservedAt: time.Now().UTC(),
			}
		} else if isUnexpectedTerminalStatus(event.Status) {
			appendInstanceEvent(record, event)
			s.beginRecoveryLocked(id, record, instance, event.Message)
			s.mu.Unlock()
			return
		} else if event.Status != "" {
			record.status = event.Status
			if event.Status == "failed" {
				record.health = Health{
					Status:     orchestrator.EngineHealthUnhealthy,
					Message:    event.Message,
					ObservedAt: event.OccurredAt,
				}
			} else if event.Status == "exited" {
				record.health = Health{
					Status:     orchestrator.EngineHealthStopped,
					ObservedAt: event.OccurredAt,
				}
			}
		}
		appendInstanceEvent(record, event)
		s.mu.Unlock()
	}

	s.mu.Lock()
	current, ok := s.instances[id]
	if ok && current == record && record.instance == instance && record.status == "connected" {
		s.beginRecoveryLocked(id, record, instance, "interaction engine event stream closed")
	}
	s.mu.Unlock()
}

func isUnexpectedTerminalStatus(status string) bool {
	switch status {
	case "disconnected", "exited", "failed":
		return true
	default:
		return false
	}
}

// beginRecoveryLocked preserves the desired interaction attachment while
// replacing only Jangolova's failed adapter instance. It never launches,
// restarts, stops, or otherwise supervises the caller-owned target.
func (s *Service) beginRecoveryLocked(
	id string,
	record *runningInstance,
	failed orchestrator.EngineInstance,
	message string,
) {
	if record.status == "disconnecting" || record.status == "recovering" || record.engine == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	record.recoveryCancel = cancel
	record.recoveryDone = make(chan struct{})
	record.status = "recovering"
	record.health = Health{
		Status:     orchestrator.EngineHealthStarting,
		Message:    "reattaching interaction engine to caller-owned target",
		ObservedAt: time.Now().UTC(),
	}
	record.healthFailures = 0
	if record.redact != nil {
		message = record.redact(message)
	}
	appendInstanceEvent(record, orchestrator.EngineEvent{
		Type: "instance.recovering", Status: "recovering", Message: message,
		OccurredAt: time.Now().UTC(),
	})
	go s.recoverInstance(ctx, id, record, failed)
}

func (s *Service) recoverInstance(
	ctx context.Context,
	id string,
	record *runningInstance,
	failed orchestrator.EngineInstance,
) {
	defer close(record.recoveryDone)
	if failed != nil {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = failed.Disconnect(disconnectCtx)
		cancel()
	}

	backoff := s.recoveryInitialBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		connectCtx, cancel := context.WithTimeout(ctx, s.recoveryConnectTimeout)
		candidate, err := record.engine.Connect(connectCtx, record.spec, record.target)
		cancel()
		if err == nil && candidate == nil {
			err = errors.New("interaction engine returned no instance")
		}
		if err == nil {
			s.mu.Lock()
			current, exists := s.instances[id]
			if !exists || current != record || record.status != "recovering" || ctx.Err() != nil {
				s.mu.Unlock()
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = candidate.Disconnect(cleanupCtx)
				cleanupCancel()
				return
			}
			record.instance = candidate
			record.recoveryCancel = nil
			record.status = "connected"
			record.healthFailures = 0
			record.health = Health{Status: orchestrator.EngineHealthHealthy, ObservedAt: time.Now().UTC()}
			appendInstanceEvent(record, orchestrator.EngineEvent{
				Type: "instance.recovered", Status: "connected", OccurredAt: time.Now().UTC(),
			})
			s.mu.Unlock()
			if source, ok := candidate.(orchestrator.EngineEventSource); ok {
				if events := source.EngineEvents(); events != nil {
					go s.watchInstanceEvents(id, record, candidate, events)
				}
			}
			return
		}

		message := err.Error()
		if record.redact != nil {
			message = record.redact(message)
		}
		s.mu.Lock()
		current, exists := s.instances[id]
		if !exists || current != record || record.status != "recovering" || ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		appendInstanceEvent(record, orchestrator.EngineEvent{
			Type: "instance.recovery.retrying", Status: "recovering", Message: message,
			OccurredAt: time.Now().UTC(),
		})
		s.mu.Unlock()

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff *= 2
		if backoff > s.recoveryMaximumBackoff {
			backoff = s.recoveryMaximumBackoff
		}
	}
}

func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	type managedInstance struct {
		instance     orchestrator.EngineInstance
		release      func(context.Context) error
		redact       func(string) string
		recoveryDone <-chan struct{}
	}
	values := make([]managedInstance, 0, len(s.instances))
	for _, record := range s.instances {
		if record.recoveryCancel != nil {
			record.recoveryCancel()
			record.recoveryCancel = nil
		}
		values = append(values, managedInstance{instance: record.instance, release: record.release, redact: record.redact, recoveryDone: record.recoveryDone})
	}
	s.instances = make(map[string]*runningInstance)
	s.mu.Unlock()
	var problems []error
	for index := len(values) - 1; index >= 0; index-- {
		value := values[index]
		if value.recoveryDone != nil {
			select {
			case <-value.recoveryDone:
			case <-ctx.Done():
				problems = append(problems, errors.New("cancel interaction recovery: "+ctx.Err().Error()))
				continue
			}
		}
		if value.instance != nil {
			if err := value.instance.Disconnect(ctx); err != nil {
				if value.redact != nil {
					err = errors.New(value.redact(err.Error()))
				}
				problems = append(problems, err)
			}
		}
		if value.release != nil {
			if err := value.release(ctx); err != nil {
				problems = append(problems, errors.New("release target connection material"))
			}
		}
	}
	return errors.Join(problems...)
}

func validateConnectRequest(request ConnectRequest) error {
	if request.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion must be %q", APIVersion)
	}
	if !instanceIDPattern.MatchString(request.InstanceID) {
		return errors.New("instanceId must be a lowercase DNS-style name")
	}
	if strings.TrimSpace(request.Engine.Adapter) == "" {
		return errors.New("engine.adapter is required")
	}
	for _, capability := range request.Engine.RequiredCapabilities {
		if !handleNamePattern.MatchString(capability) {
			return fmt.Errorf("invalid required engine capability %q", capability)
		}
	}
	for _, action := range request.Engine.Approval.RequiredActions {
		if !handleNamePattern.MatchString(action) {
			return fmt.Errorf("invalid approval-required action %q", action)
		}
	}
	if request.Target.APIVersion != "" && request.Target.APIVersion != TargetAPIVersion {
		return fmt.Errorf("target.apiVersion must be %q", TargetAPIVersion)
	}
	if request.Target.APIVersion != "" && request.Target.TargetID == "" {
		return errors.New("target.targetId is required when target.apiVersion is supplied")
	}
	if request.Target.TargetID != "" && !targetIDPattern.MatchString(request.Target.TargetID) {
		return errors.New("target.targetId is invalid")
	}
	if strings.TrimSpace(request.Target.Kind) == "" {
		return errors.New("target.kind is required")
	}
	if len(request.Engine.Options) != 0 {
		var object map[string]any
		if err := json.Unmarshal(request.Engine.Options, &object); err != nil || object == nil {
			return errors.New("engine.options must be a JSON object")
		}
	}
	if len(request.Target.Endpoints) > 64 {
		return errors.New("target endpoints must not exceed 64 entries")
	}
	endpointNames := make(map[string]struct{}, len(request.Target.Endpoints))
	for index, endpoint := range request.Target.Endpoints {
		if !handleNamePattern.MatchString(endpoint.Name) {
			return fmt.Errorf("invalid target endpoint name %q", endpoint.Name)
		}
		if _, exists := endpointNames[endpoint.Name]; exists {
			return fmt.Errorf("duplicate target endpoint name %q", endpoint.Name)
		}
		endpointNames[endpoint.Name] = struct{}{}
		if strings.TrimSpace(endpoint.Protocol) == "" {
			return fmt.Errorf("target endpoint %d protocol is required", index)
		}
		if strings.TrimSpace(endpoint.URL) == "" || len(endpoint.URL) > 4096 || strings.ContainsRune(endpoint.URL, '\x00') {
			return fmt.Errorf("target endpoint %q URL is required", endpoint.Name)
		}
		if endpoint.CredentialRef != "" && !handleNamePattern.MatchString(endpoint.CredentialRef) {
			return fmt.Errorf("target endpoint %q credentialRef is invalid", endpoint.Name)
		}
		if endpoint.TLSRef != "" && !handleNamePattern.MatchString(endpoint.TLSRef) {
			return fmt.Errorf("target endpoint %q tlsRef is invalid", endpoint.Name)
		}
		if endpoint.Audience != "" && endpoint.Audience != "engine" && endpoint.Audience != "target" {
			return fmt.Errorf("target endpoint %q audience must be engine or target", endpoint.Name)
		}
		if err := validateStringMetadata("target endpoint "+endpoint.Name+" metadata", endpoint.Metadata); err != nil {
			return err
		}
	}
	for name, value := range request.Target.Handles {
		if !handleNamePattern.MatchString(name) {
			return fmt.Errorf("invalid handle name %q", name)
		}
		if value == "" {
			return fmt.Errorf("handle %q is empty", name)
		}
		if strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("handle %q contains a null byte", name)
		}
	}
	if err := validateStringMetadata("target metadata", request.Target.Metadata); err != nil {
		return err
	}
	return nil
}

func validateStringMetadata(label string, values map[string]string) error {
	for name, value := range values {
		if !handleNamePattern.MatchString(name) {
			return fmt.Errorf("%s key %q is invalid", label, name)
		}
		if strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("%s value %q contains a null byte", label, name)
		}
	}
	return nil
}

func describeInstance(id string, record *runningInstance) Instance {
	capabilities := []string{}
	if provider, ok := record.instance.(orchestrator.EngineCapabilityProvider); ok {
		capabilities = stableCapabilities(provider.EngineCapabilities())
	}
	return Instance{
		APIVersion:   APIVersion,
		InstanceID:   id,
		Adapter:      record.adapter,
		Status:       record.status,
		Health:       record.health,
		Capabilities: capabilities,
	}
}

func probeInstanceHealth(
	ctx context.Context,
	instance orchestrator.EngineInstance,
) orchestrator.EngineHealth {
	provider, ok := instance.(orchestrator.EngineHealthProvider)
	if !ok {
		return orchestrator.EngineHealth{
			Status:     orchestrator.EngineHealthUnknown,
			Message:    "interaction engine does not implement an active health probe",
			ObservedAt: time.Now().UTC(),
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return provider.EngineHealth(probeCtx)
}

func updateInstanceHealth(record *runningInstance, health orchestrator.EngineHealth) {
	if health.ObservedAt.IsZero() {
		health.ObservedAt = time.Now().UTC()
	}
	changed := record.health.Status != health.Status || record.health.Message != health.Message
	record.health = Health{
		Status:     health.Status,
		Message:    health.Message,
		ObservedAt: health.ObservedAt,
	}
	if changed {
		appendInstanceEvent(record, orchestrator.EngineEvent{
			Type:       "engine.health." + health.Status,
			Message:    health.Message,
			OccurredAt: health.ObservedAt,
		})
	}
}

func appendInstanceEvent(record *runningInstance, event orchestrator.EngineEvent) {
	record.nextEvent++
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	record.events = append(record.events, InstanceEvent{
		Cursor:     strconv.FormatUint(record.nextEvent, 10),
		Type:       event.Type,
		Status:     event.Status,
		Message:    event.Message,
		OccurredAt: event.OccurredAt,
	})
	if len(record.events) > eventHistoryLimit {
		record.events = append([]InstanceEvent(nil), record.events[len(record.events)-eventHistoryLimit:]...)
	}
}

func parseCursor(value string) (uint64, error) {
	if value == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errors.New("event cursor must be a non-negative integer")
	}
	return cursor, nil
}

func parseEventLimit(value string) (int, error) {
	if value == "" {
		return 100, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 || limit > eventHistoryLimit {
		return 0, fmt.Errorf("event limit must be between 1 and %d", eventHistoryLimit)
	}
	return limit, nil
}

func cloneValues(value map[string]string) map[string]string {
	cloned := make(map[string]string, len(value))
	for name, item := range value {
		cloned[name] = item
	}
	return cloned
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{Code: code, Message: message})
}

// connectFailure classifies why connectAndRecord could not establish an
// interaction instance so callers can map it to distinct HTTP responses.
type connectFailure struct {
	code    string
	message string
}

func (f *connectFailure) Error() string { return f.message }

// authorizeAction extracts the action name from act params and delegates to
// the instance's Authorize per-capability policy gate.
func authorizeAction(ctx context.Context, instance orchestrator.EngineInstance, params json.RawMessage) error {
	var action struct {
		Name     string          `json:"name"`
		TargetID string          `json:"targetId,omitempty"`
		Input    json.RawMessage `json:"input,omitempty"`
	}
	if err := json.Unmarshal(params, &action); err != nil {
		return errors.New("cannot decode action for policy authorization")
	}
	if strings.TrimSpace(action.Name) == "" {
		return errors.New("action name is required for authorization")
	}
	decision, err := instance.Authorize(ctx, orchestrator.AuthorizeRequest{
		Action:   action.Name,
		TargetID: action.TargetID,
		Input:    action.Input,
	})
	if err != nil {
		return err
	}
	if !decision.Authorized {
		if decision.Reason != "" {
			return errors.New(decision.Reason)
		}
		return fmt.Errorf("action %q is not authorized", action.Name)
	}
	return nil
}

func actionNameForAudit(params json.RawMessage) string {
	var action struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &action); err != nil || strings.TrimSpace(action.Name) == "" {
		return "unknown"
	}
	return action.Name
}

func (s *Service) appendActionAudit(id string, record *runningInstance, status, action, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.instances[id]; !ok || current != record {
		return
	}
	if record.redact != nil && message != "" {
		message = record.redact(message)
	}
	if action != "" && action != "unknown" {
		if message != "" {
			message = action + ": " + message
		} else {
			message = action
		}
	}
	appendInstanceEvent(record, orchestrator.EngineEvent{
		Type:       "action." + status,
		Status:     status,
		Message:    message,
		OccurredAt: time.Now().UTC(),
	})
}

func (s *Service) handleApprovalRequest(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var request ApprovalRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || !json.Valid(request.Params) {
		writeError(w, http.StatusBadRequest, "invalid_request", "approval params must be valid JSON")
		return
	}
	action := actionNameForAudit(request.Params)
	if action == "unknown" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", "approval action name is required")
		return
	}
	s.mu.Lock()
	record, ok := s.instances[id]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "instance_not_found", "interaction instance was not found")
		return
	}
	if !requiresApproval(record.request.Engine.Approval, action) {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "approval_not_required", "this action does not require approval")
		return
	}
	instance := record.instance
	s.mu.Unlock()
	if err := authorizeAction(r.Context(), instance, request.Params); err != nil {
		s.appendActionAudit(id, record, "denied", action, err.Error())
		writeError(w, http.StatusForbidden, "policy_denied", err.Error())
		return
	}
	lifetime := defaultApprovalLifetime
	if request.ExpiresInSeconds != 0 {
		if request.ExpiresInSeconds < 1 || request.ExpiresInSeconds > 3600 {
			writeError(w, http.StatusUnprocessableEntity, "invalid_request", "approval expiry must be between 1 and 3600 seconds")
			return
		}
		lifetime = time.Duration(request.ExpiresInSeconds) * time.Second
	}
	approval := Approval{
		APIVersion: APIVersion, InstanceID: id, ApprovalID: newApprovalID(), Action: action,
		Status: "pending", ExpiresAt: time.Now().UTC().Add(lifetime),
	}
	s.mu.Lock()
	if current, exists := s.instances[id]; !exists || current != record {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "instance_not_found", "interaction instance was not found")
		return
	}
	record.approvals[approval.ApprovalID] = &pendingApproval{
		action: action, digest: actionDigest(action, request.Params), status: approval.Status, expires: approval.ExpiresAt,
	}
	appendInstanceEvent(record, orchestrator.EngineEvent{Type: "action.approval_requested", Status: "pending", Message: action, OccurredAt: time.Now().UTC()})
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, approval)
}

func (s *Service) handleApprovalResolution(w http.ResponseWriter, r *http.Request, id, approvalID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var resolution ApprovalResolution
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&resolution); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid approval resolution")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.instances[id]
	if !ok {
		writeError(w, http.StatusNotFound, "instance_not_found", "interaction instance was not found")
		return
	}
	pending, ok := record.approvals[approvalID]
	if !ok || pending.consumed || pending.status != "pending" || time.Now().After(pending.expires) {
		writeError(w, http.StatusNotFound, "approval_not_found", "approval was not found or has expired")
		return
	}
	if resolution.Approved {
		pending.status = "approved"
	} else {
		pending.status = "rejected"
	}
	appendInstanceEvent(record, orchestrator.EngineEvent{
		Type: "action.approval_" + pending.status, Status: pending.status, Message: pending.action, OccurredAt: time.Now().UTC(),
	})
	writeJSON(w, http.StatusOK, Approval{
		APIVersion: APIVersion, InstanceID: id, ApprovalID: approvalID, Action: pending.action,
		Status: pending.status, ExpiresAt: pending.expires,
	})
}

func requiresApproval(policy ApprovalPolicy, action string) bool {
	for _, candidate := range policy.RequiredActions {
		if candidate == action {
			return true
		}
	}
	return false
}

func (s *Service) consumeApproval(id string, record *runningInstance, approvalID, action string, params json.RawMessage) error {
	if strings.TrimSpace(approvalID) == "" {
		return fmt.Errorf("action %q requires an approved approvalId", action)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.instances[id]; !ok || current != record {
		return errors.New("interaction instance was not found")
	}
	pending, ok := record.approvals[approvalID]
	if !ok || pending.consumed || pending.status != "approved" || time.Now().After(pending.expires) {
		return errors.New("approvalId is not approved or has expired")
	}
	if pending.action != action || pending.digest != actionDigest(action, params) {
		return errors.New("approvalId does not match this action")
	}
	pending.consumed = true
	appendInstanceEvent(record, orchestrator.EngineEvent{Type: "action.approval_consumed", Status: "approved", Message: action, OccurredAt: time.Now().UTC()})
	return nil
}

func actionDigest(action string, params json.RawMessage) string {
	sum := sha256.Sum256(append([]byte(action+":"), params...))
	return hex.EncodeToString(sum[:])
}

func newApprovalID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("approval-%d", time.Now().UnixNano())
	}
	return "approval-" + hex.EncodeToString(value)
}

// removeInstance deletes the instance record under id if present.
func (s *Service) removeInstance(id string) {
	s.mu.Lock()
	delete(s.instances, id)
	s.mu.Unlock()
}

// connectAndRecord attaches adapter to the caller-owned target described by
// request and records the resulting interaction instance under id. On success
// it returns the connected record with a nil error. On failure it removes any
// placeholder record and returns a *connectFailure carrying the redacted
// detail message. Reaching the connected state also starts the asynchronous
// event watcher so lifecycle and recovery events remain available.
func (s *Service) connectAndRecord(
	ctx context.Context,
	id, adapterName string,
	request ConnectRequest,
	adapter orchestrator.EngineAdapter,
) (*runningInstance, error) {
	s.mu.Lock()
	if _, exists := s.instances[id]; exists {
		s.mu.Unlock()
		return nil, &connectFailure{code: "instance_exists", message: "interaction instance already exists"}
	}
	record := &runningInstance{
		request:   request,
		adapter:   adapterName,
		status:    "connecting",
		health:    Health{Status: orchestrator.EngineHealthStarting, ObservedAt: time.Now().UTC()},
		approvals: make(map[string]*pendingApproval),
	}
	appendInstanceEvent(record, orchestrator.EngineEvent{
		Type:       "instance.connecting",
		Status:     "connecting",
		OccurredAt: time.Now().UTC(),
	})
	s.instances[id] = record
	s.mu.Unlock()

	endpoints := make([]orchestrator.TargetEndpoint, 0, len(request.Target.Endpoints))
	for _, endpoint := range request.Target.Endpoints {
		endpoints = append(endpoints, orchestrator.TargetEndpoint{
			Name: endpoint.Name, Protocol: endpoint.Protocol, URL: endpoint.URL,
			CredentialRef: endpoint.CredentialRef, TLSRef: endpoint.TLSRef,
			Audience: endpoint.Audience, Metadata: cloneValues(endpoint.Metadata),
		})
	}
	target := orchestrator.EngineTarget{
		APIVersion: request.Target.APIVersion,
		TargetID:   request.Target.TargetID,
		Kind:       request.Target.Kind,
		Endpoints:  endpoints,
		Handles:    orchestrator.EngineHandles(cloneValues(request.Target.Handles)),
		Metadata:   cloneValues(request.Target.Metadata),
	}
	preparedTarget, release, err := targetconn.Prepare(ctx, s.resolver, target)
	if err != nil {
		s.removeInstance(id)
		return nil, &connectFailure{code: "target_resolution_failed", message: err.Error()}
	}
	s.mu.Lock()
	record.release = release
	record.redact = func(message string) string { return targetconn.RedactString(message, preparedTarget) }
	record.redactJSON = func(value json.RawMessage) json.RawMessage { return targetconn.RedactJSON(value, preparedTarget) }
	s.mu.Unlock()

	spec := manifest.EngineSpec{
		Adapter:              adapterName,
		RequiredCapabilities: append([]string(nil), request.Engine.RequiredCapabilities...),
		Source:               request.Engine.Source,
		Options:              request.Engine.Options,
	}
	instance, err := adapter.Connect(ctx, spec, preparedTarget)
	if err != nil {
		err = targetconn.Redact(err, preparedTarget)
		_ = release(context.Background())
		s.removeInstance(id)
		return nil, &connectFailure{code: "engine_connect_failed", message: err.Error()}
	}
	if instance == nil {
		_ = release(context.Background())
		s.removeInstance(id)
		return nil, &connectFailure{code: "engine_connect_failed", message: "interaction engine returned no instance"}
	}

	s.mu.Lock()
	record.instance = instance
	record.engine = adapter
	record.spec = spec
	record.target = preparedTarget
	record.status = "connected"
	record.health = Health{Status: orchestrator.EngineHealthHealthy, ObservedAt: time.Now().UTC()}
	appendInstanceEvent(record, orchestrator.EngineEvent{
		Type:       "instance.connected",
		Status:     "connected",
		OccurredAt: time.Now().UTC(),
	})
	s.mu.Unlock()

	if source, ok := instance.(orchestrator.EngineEventSource); ok {
		if events := source.EngineEvents(); events != nil {
			go s.watchInstanceEvents(id, record, instance, events)
		}
	}
	return record, nil
}

func (s *Service) handleReconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req ReconcileRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid reconcile request")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "reconcile request must contain one JSON value")
		return
	}

	// desiredMap keeps the first occurrence of each instance ID; duplicates are
	// reported as failures so no instance is connected twice.
	desiredMap := make(map[string]ConnectRequest, len(req.Desired))
	failed := map[string]string{}
	for _, cr := range req.Desired {
		if _, seen := desiredMap[cr.InstanceID]; seen {
			if _, alreadyFailed := failed[cr.InstanceID]; !alreadyFailed {
				failed[cr.InstanceID] = "duplicate desired instance"
			}
			continue
		}
		desiredMap[cr.InstanceID] = cr
	}

	created := []string{}
	retained := []string{}
	for id, cr := range desiredMap {
		s.mu.Lock()
		_, exists := s.instances[id]
		s.mu.Unlock()
		if exists {
			retained = append(retained, id)
			continue
		}
		if err := validateConnectRequest(cr); err != nil {
			failed[id] = err.Error()
			continue
		}
		adapterName := strings.TrimSpace(cr.Engine.Adapter)
		if adapterName == "auto" {
			selected, selectErr := SelectAutomaticEngine(r.Context(), s.registry, cr.Target, cr.Engine.RequiredCapabilities)
			if selectErr != nil {
				failed[id] = selectErr.Error()
				continue
			}
			adapterName = selected
		}
		adapter, ok := s.registry.Engine(adapterName)
		if !ok {
			failed[id] = "interaction engine is not registered"
			continue
		}
		if _, err := s.connectAndRecord(r.Context(), id, adapterName, cr, adapter); err != nil {
			var failure *connectFailure
			if errors.As(err, &failure) {
				failed[id] = failure.message
			} else {
				failed[id] = err.Error()
			}
			continue
		}
		created = append(created, id)
	}

	pruned := []string{}
	if req.Prune {
		type managedInstance struct {
			instance     orchestrator.EngineInstance
			release      func(context.Context) error
			recoveryDone chan struct{}
		}
		var values []managedInstance
		s.mu.Lock()
		for id, record := range s.instances {
			if _, keep := desiredMap[id]; keep {
				continue
			}
			delete(s.instances, id)
			pruned = append(pruned, id)
			if record.recoveryCancel != nil {
				record.recoveryCancel()
				record.recoveryCancel = nil
			}
			values = append(values, managedInstance{instance: record.instance, release: record.release, recoveryDone: record.recoveryDone})
		}
		s.mu.Unlock()
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, value := range values {
			if value.recoveryDone != nil {
				select {
				case <-value.recoveryDone:
				case <-disconnectCtx.Done():
				}
			}
			if value.instance != nil {
				_ = value.instance.Disconnect(disconnectCtx)
			}
			if value.release != nil {
				_ = value.release(disconnectCtx)
			}
		}
	}

	s.mu.Lock()
	reconciled := len(s.instances)
	s.mu.Unlock()
	resp := ReconcileResponse{
		APIVersion: APIVersion,
		Reconciled: reconciled,
		Created:    created,
		Retained:   retained,
		Pruned:     pruned,
		Failed:     failed,
	}
	writeJSON(w, http.StatusOK, resp)
}
