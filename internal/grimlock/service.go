package grimlock

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"

	"jangolova/internal/blockade"
	"jangolova/internal/bridge"
	"jangolova/internal/engineprovider"
	"jangolova/internal/manifest"
	"jangolova/internal/orchestrator"
	"jangolova/targetconn"
)

const (
	grimlockAppName    = "grimlock"
	grimlockEventLimit = 256
	maxJSONBody        = 1024 * 1024
)

var (
	userIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	targetIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)
	handlePattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)
)

// Service is Grimlock's northbound application boundary. Protocol adapters
// should call Routes or the service methods rather than implementing their own
// model, policy, approval, or session lifecycle logic.
type Service struct {
	mu             sync.Mutex
	runtime        *Runtime
	registry       *orchestrator.Registry
	token          string
	resolver       targetconn.Resolver
	sessionService session.Service
	sessions       map[string]*runningSession
	store          *sessionStore
	blockadeClient *blockade.Client
	blockadePool   *blockade.WorkerPool
	blockadeEngine blockade.Engine
}

type ServiceOption func(*Service)

func WithTargetResolver(resolver targetconn.Resolver) ServiceOption {
	return func(service *Service) {
		if resolver != nil {
			service.resolver = resolver
		}
	}
}

// WithBlockadeClient enables the read-only Blockade observation tool for new
// Grimlock sessions. The worker remains external to Jangolova.
func WithBlockadeClient(client blockade.Client) ServiceOption {
	return func(service *Service) { service.blockadeClient = &client }
}

func WithBlockadeWorkerPool(pool *blockade.WorkerPool) ServiceOption {
	return func(service *Service) {
		if pool != nil {
			service.blockadePool = pool
			service.blockadeClient = &blockade.Client{WorkerPool: pool}
		}
	}
}

// WithBlockadeEngine enables the Blockade observation tool backed by any
// configured engine kind, including in-process native engines.
func WithBlockadeEngine(engine blockade.Engine) ServiceOption {
	return func(service *Service) {
		if engine != nil {
			service.blockadeEngine = engine
			service.blockadeClient = &blockade.Client{Engine: engine}
		}
	}
}

// WithStoreDirectory enables persistent session storage in the given
// directory. Sessions are saved as individual JSON files and reloaded on
// service start so that session summaries and event history survive restarts.
func WithStoreDirectory(dir string) ServiceOption {
	return func(service *Service) {
		if dir == "" {
			return
		}
		store, err := newSessionStore(dir)
		if err != nil {
			// Log but don't fail — the service can still run without persistence.
			return
		}
		service.store = store
		// Load persisted sessions into the service's session map.
		for id, rec := range store.sessions {
			service.sessions[id] = rec
		}
	}
}

func NewService(runtime *Runtime, registry *orchestrator.Registry, token string, options ...ServiceOption) (*Service, error) {
	if runtime == nil {
		return nil, errors.New("Grimlock runtime is required")
	}
	if registry == nil {
		return nil, errors.New("Grimlock engine registry is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("Grimlock service token is required")
	}
	service := &Service{
		runtime:        runtime,
		registry:       registry,
		token:          token,
		resolver:       targetconn.DefaultResolver(),
		sessionService: session.InMemoryService(),
		sessions:       make(map[string]*runningSession),
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service, nil
}

type interactionAttachment struct {
	instance orchestrator.EngineInstance
	release  func(context.Context) error
	redact   func(string) string
}

type runningSession struct {
	mu sync.Mutex

	summary      SessionView
	runner       *runner.Runner
	agent        *AgentSession
	attachments  []interactionAttachment
	status       string
	closed       bool
	activeCancel context.CancelFunc
	activeDone   chan struct{}

	pending    map[string]PendingApproval
	events     []EventEnvelope
	nextCursor uint64

	persist func()
}

func (s *Service) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/connectors", s.handleConnectors)
	mux.HandleFunc("/v1/sessions", s.handleSessions)
	mux.HandleFunc("/v1/sessions/", s.handleSession)
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
		"ok": true, "service": "jangolova-grimlock", "apiVersion": APIVersion,
	})
}

func (s *Service) handleConnectors(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/connectors" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	protocols := s.runtime.ModelProtocols()
	connectors := make([]ConnectorDescriptor, 0, len(protocols))
	for _, protocol := range protocols {
		connectors = append(connectors, ConnectorDescriptor{Protocol: protocol})
	}
	writeJSON(w, http.StatusOK, map[string]any{"apiVersion": APIVersion, "connectors": connectors})
}

func (s *Service) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/sessions" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var request CreateSessionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid session request")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "session request must contain one JSON value")
		return
	}
	if err := validateCreateSessionRequest(request); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", err.Error())
		return
	}
	if err := s.createSession(r.Context(), request); err != nil {
		status := http.StatusBadGateway
		code := "session_create_failed"
		if errors.Is(err, errSessionExists) {
			status, code = http.StatusConflict, "session_exists"
		}
		writeError(w, status, code, err.Error())
		return
	}
	s.mu.Lock()
	record := s.sessions[request.Agent.SessionID]
	summary := record.snapshot()
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, summary)
}

func (s *Service) handleSession(w http.ResponseWriter, r *http.Request) {
	relative := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"), "/")
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || !sessionIDPattern.MatchString(parts[0]) {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		s.handleSessionRoot(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "run" {
		s.handleRun(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "events" {
		s.handleEvents(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "cancel" {
		s.handleCancel(w, r, id)
		return
	}
	if len(parts) == 3 && parts[1] == "confirmations" && parts[2] != "" {
		s.handleConfirmation(w, r, id, parts[2])
		return
	}
	http.NotFound(w, r)
}

func (s *Service) handleSessionRoot(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	record, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "session_not_found", "Grimlock session was not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, record.snapshot())
	case http.MethodDelete:
		s.mu.Lock()
		if current, exists := s.sessions[id]; !exists || current != record {
			s.mu.Unlock()
			writeError(w, http.StatusNotFound, "session_not_found", "Grimlock session was not found")
			return
		}
		delete(s.sessions, id)
		s.mu.Unlock()
		if err := s.deleteStoredSession(id); err != nil {
			writeError(w, http.StatusBadGateway, "session_delete_failed", err.Error())
			return
		}
		if err := s.closeSession(r.Context(), record); err != nil {
			writeError(w, http.StatusBadGateway, "session_close_failed", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Service) handleRun(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var request RunRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid run request")
		return
	}
	if request.APIVersion != "" && request.APIVersion != APIVersion {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", fmt.Sprintf("apiVersion must be %q", APIVersion))
		return
	}
	if strings.TrimSpace(request.Text) == "" || len(request.Text) > 256*1024 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", "text is required and must not exceed 256 KiB")
		return
	}
	record, ok := s.lookupSession(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session_not_found", "Grimlock session was not found")
		return
	}
	// Persisted sessions restore metadata and event history, but active model
	// and interaction attachments are intentionally not serialized. Refuse a
	// run until the caller creates a fresh attached session rather than
	// dereferencing a nil runner after a restart.
	if record.runner == nil {
		writeError(w, http.StatusConflict, "session_reconnect_required", "Grimlock session requires model and target reconnection after restart")
		return
	}
	content := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: request.Text}}}
	s.runHTTP(w, r, record, content, request.Stream)
}

func (s *Service) handleConfirmation(w http.ResponseWriter, r *http.Request, id, approvalID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var request ConfirmationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid confirmation request")
		return
	}
	if request.APIVersion != "" && request.APIVersion != APIVersion {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", fmt.Sprintf("apiVersion must be %q", APIVersion))
		return
	}
	record, ok := s.lookupSession(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session_not_found", "Grimlock session was not found")
		return
	}
	record.mu.Lock()
	pending, exists := record.pending[approvalID]
	if exists {
		delete(record.pending, approvalID)
	}
	record.mu.Unlock()
	if !exists {
		writeError(w, http.StatusNotFound, "approval_not_found", "approval request was not found or already resolved")
		return
	}
	content := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: approvalID, Name: toolconfirmation.FunctionCallName,
		Response: map[string]any{"confirmed": request.Confirmed},
	}}}}
	if err := s.runAndRespond(w, r, record, content); err != nil {
		record.mu.Lock()
		record.pending[approvalID] = pending
		record.mu.Unlock()
		writeError(w, http.StatusBadGateway, "run_failed", err.Error())
	}
}

func (s *Service) handleEvents(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	after, err := parseGrimlockCursor(r.URL.Query().Get("after"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_cursor", err.Error())
		return
	}
	limit, err := parseGrimlockLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_limit", err.Error())
		return
	}
	record, ok := s.lookupSession(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session_not_found", "Grimlock session was not found")
		return
	}
	record.mu.Lock()
	if after > record.nextCursor {
		record.mu.Unlock()
		writeError(w, http.StatusUnprocessableEntity, "invalid_cursor", "event cursor is ahead of the session")
		return
	}
	if len(record.events) != 0 {
		first, _ := strconv.ParseUint(record.events[0].Cursor, 10, 64)
		if after+1 < first {
			record.mu.Unlock()
			writeError(w, http.StatusGone, "cursor_expired", "event cursor is older than retained history")
			return
		}
	}
	events := make([]EventEnvelope, 0, limit)
	cursor := record.nextCursor
	for _, event := range record.events {
		sequence, _ := strconv.ParseUint(event.Cursor, 10, 64)
		if sequence <= after {
			continue
		}
		events = append(events, cloneEventEnvelope(event))
		cursor = sequence
		if len(events) == limit {
			break
		}
	}
	record.mu.Unlock()
	writeJSON(w, http.StatusOK, EventsResponse{APIVersion: APIVersion, SessionID: id, Cursor: strconv.FormatUint(cursor, 10), Events: events})
}

func (s *Service) handleCancel(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	record, ok := s.lookupSession(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session_not_found", "Grimlock session was not found")
		return
	}
	record.mu.Lock()
	cancel := record.activeCancel
	record.mu.Unlock()
	if cancel == nil {
		writeError(w, http.StatusConflict, "run_not_active", "Grimlock session has no active run")
		return
	}
	cancel()
	record.mu.Lock()
	summary := record.snapshotLocked()
	record.mu.Unlock()
	writeJSON(w, http.StatusOK, summary)
}

func (s *Service) createSession(ctx context.Context, request CreateSessionRequest) error {
	if _, exists := s.lookupSession(request.Agent.SessionID); exists {
		return errSessionExists
	}
	attachments := make([]interactionAttachment, 0, len(request.Bindings))
	bindings := make([]InteractionBinding, 0, len(request.Bindings))
	for _, binding := range request.Bindings {
		adapterName := strings.TrimSpace(binding.Engine.Adapter)
		if adapterName == "auto" {
			selected, err := engineprovider.SelectAutomaticEngine(ctx, s.registry, binding.Target, binding.Engine.RequiredCapabilities)
			if err != nil {
				cleanupAttachments(ctx, attachments)
				return err
			}
			adapterName = selected
		}
		adapter, ok := s.registry.Engine(adapterName)
		if !ok {
			cleanupAttachments(ctx, attachments)
			return fmt.Errorf("interaction engine %q is not registered", adapterName)
		}
		target := engineTarget(binding.Target)
		prepared, release, err := targetconn.Prepare(ctx, s.resolver, target)
		if err != nil {
			cleanupAttachments(ctx, attachments)
			return fmt.Errorf("resolve interaction %q target: %w", binding.InteractionID, err)
		}
		spec := manifest.EngineSpec{
			Adapter: adapterName, RequiredCapabilities: append([]string(nil), binding.Engine.RequiredCapabilities...),
			Source: binding.Engine.Source, Options: binding.Engine.Options,
		}
		instance, err := adapter.Connect(ctx, spec, prepared)
		if err != nil {
			_ = release(context.Background())
			cleanupAttachments(ctx, attachments)
			return fmt.Errorf("connect interaction %q: %w", binding.InteractionID, targetconn.Redact(err, prepared))
		}
		if instance == nil {
			_ = release(context.Background())
			cleanupAttachments(ctx, attachments)
			return fmt.Errorf("connect interaction %q: engine returned no instance", binding.InteractionID)
		}
		caller, ok := instance.(bridge.Caller)
		if !ok {
			_ = instance.Disconnect(context.Background())
			_ = release(context.Background())
			cleanupAttachments(ctx, attachments)
			return fmt.Errorf("interaction %q does not support bridge calls", binding.InteractionID)
		}
		attachments = append(attachments, interactionAttachment{
			instance: instance, release: release,
			redact: func(message string) string { return targetconn.RedactString(message, prepared) },
		})
		policy, requireApproval := bindingPolicy(binding.AllowWrites)
		bindings = append(bindings, InteractionBinding{
			InteractionID: binding.InteractionID, Caller: caller,
			AllowedCapabilities: append([]string(nil), binding.AllowedCapabilities...),
			Policy:              policy, RequireApproval: requireApproval,
		})
	}
	var extraTools []tool.Tool
	if s.blockadeClient != nil {
		var err error
		extraTools, err = BlockadeTools(*s.blockadeClient)
		if err != nil {
			_ = cleanupAttachments(ctx, attachments)
			return fmt.Errorf("configure Blockade: %w", err)
		}
	}
	agentSession, err := s.runtime.CreateInteractionAgentWithTools(ctx, request.Agent, bindings, extraTools)
	if err != nil {
		_ = cleanupAttachments(ctx, attachments)
		return err
	}
	agentRunner, err := runner.New(runner.Config{
		AppName: grimlockAppName, Agent: agentSession.Agent,
		SessionService: s.sessionService, AutoCreateSession: true,
	})
	if err != nil {
		_ = agentSession.Close(context.Background())
		_ = cleanupAttachments(ctx, attachments)
		return fmt.Errorf("create Grimlock runner: %w", err)
	}
	record := &runningSession{
		summary: SessionView{APIVersion: APIVersion, SessionID: request.Agent.SessionID,
			UserID: request.UserID, Status: "ready", CreatedAt: time.Now().UTC(), Model: request.Agent.Model},
		runner: agentRunner, agent: agentSession, attachments: attachments,
		status: "ready", pending: make(map[string]PendingApproval),
	}
	record.persist = func() { _ = s.saveSession(record) }
	s.mu.Lock()
	if _, exists := s.sessions[request.Agent.SessionID]; exists {
		s.mu.Unlock()
		_ = s.closeSession(ctx, record)
		return errSessionExists
	}
	s.sessions[request.Agent.SessionID] = record
	s.mu.Unlock()
	if err := s.saveSession(record); err != nil {
		return fmt.Errorf("persist session: %w", err)
	}
	return nil
}

func (s *Service) runHTTP(w http.ResponseWriter, r *http.Request, record *runningSession, content *genai.Content, stream bool) {
	if stream {
		s.runStream(w, r, record, content)
		return
	}
	if err := s.runAndRespond(w, r, record, content); err != nil {
		writeError(w, http.StatusBadGateway, "run_failed", err.Error())
	}
}

func (s *Service) runAndRespond(w http.ResponseWriter, r *http.Request, record *runningSession, content *genai.Content) error {
	events, err := s.execute(r.Context(), record, content, false, nil)
	if err != nil {
		return err
	}
	record.mu.Lock()
	cursor := strconv.FormatUint(record.nextCursor, 10)
	sessionID := record.summary.SessionID
	record.mu.Unlock()
	writeJSON(w, http.StatusOK, RunResponse{APIVersion: APIVersion, SessionID: sessionID, Cursor: cursor, Events: events})
	return nil
}

func (s *Service) runStream(w http.ResponseWriter, r *http.Request, record *runningSession, content *genai.Content) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusNotImplemented, "streaming_unsupported", "HTTP streaming is not supported by this server")
		return
	}
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	_, err := s.execute(r.Context(), record, content, true, func(event EventEnvelope) bool {
		payload, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return false
		}
		if _, writeErr := fmt.Fprintf(w, "id: %s\ndata: %s\n\n", event.Cursor, payload); writeErr != nil {
			return false
		}
		flusher.Flush()
		return true
	})
	if err != nil {
		payload, _ := json.Marshal(map[string]string{"message": err.Error()})
		_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
		flusher.Flush()
		return
	}
	record.mu.Lock()
	cursor := strconv.FormatUint(record.nextCursor, 10)
	record.mu.Unlock()
	_, _ = fmt.Fprintf(w, "event: done\ndata: {\"cursor\":%q}\n\n", cursor)
	flusher.Flush()
}

func (s *Service) execute(ctx context.Context, record *runningSession, content *genai.Content, streaming bool, onEvent func(EventEnvelope) bool) ([]EventEnvelope, error) {
	if record == nil || record.runner == nil {
		return nil, errors.New("Grimlock session requires model and target reconnection after restart")
	}
	ctx, cancel := context.WithCancel(ctx)
	if err := record.beginRun(cancel); err != nil {
		cancel()
		return nil, err
	}
	defer func() {
		cancel()
		record.finishRun()
	}()
	collected := make([]EventEnvelope, 0)
	mode := agent.StreamingModeNone
	if streaming {
		mode = agent.StreamingModeSSE
	}
	for event, err := range record.runner.Run(ctx, record.summary.UserID, record.summary.SessionID, content, agent.RunConfig{StreamingMode: mode}) {
		if err != nil {
			return collected, err
		}
		if event == nil {
			continue
		}
		envelope, err := record.appendEvent(event)
		if err != nil {
			return collected, err
		}
		collected = append(collected, envelope)
		if onEvent != nil && !onEvent(envelope) {
			return collected, errors.New("Grimlock event stream closed")
		}
	}
	return collected, nil
}

func (r *runningSession) beginRun(cancel context.CancelFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("Grimlock session is closed")
	}
	if r.activeCancel != nil {
		return errors.New("Grimlock session already has an active run")
	}
	r.activeCancel = cancel
	r.activeDone = make(chan struct{})
	r.status = "running"
	r.summary.Status = r.status
	return nil
}

func (r *runningSession) finishRun() {
	r.mu.Lock()
	r.activeCancel = nil
	if r.activeDone != nil {
		close(r.activeDone)
		r.activeDone = nil
	}
	if r.closed {
		r.status = "closed"
	} else if len(r.pending) != 0 {
		r.status = "waiting_approval"
	} else {
		r.status = "ready"
	}
	r.summary.Status = r.status
	r.mu.Unlock()
}

func (r *runningSession) appendEvent(event *session.Event) (EventEnvelope, error) {
	r.mu.Lock()
	raw, err := json.Marshal(event)
	if err != nil {
		r.mu.Unlock()
		return EventEnvelope{}, fmt.Errorf("encode Grimlock event: %w", err)
	}
	for id, confirmation := range event.Actions.RequestedToolConfirmations {
		r.pending[id] = PendingApproval{ID: id, Hint: confirmation.Hint, Payload: confirmation.Payload}
	}
	r.nextCursor++
	envelope := EventEnvelope{Cursor: strconv.FormatUint(r.nextCursor, 10), Event: append(json.RawMessage(nil), raw...)}
	r.events = append(r.events, envelope)
	if len(r.events) > grimlockEventLimit {
		r.events = append([]EventEnvelope(nil), r.events[len(r.events)-grimlockEventLimit:]...)
	}
	persist := r.persist
	r.mu.Unlock()
	if persist != nil {
		persist()
	}
	return envelope, nil
}

func (r *runningSession) snapshot() SessionView {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

func (r *runningSession) snapshotLocked() SessionView {
	value := r.summary
	value.Status = r.status
	value.PendingApprovals = make([]PendingApproval, 0, len(r.pending))
	for _, approval := range r.pending {
		value.PendingApprovals = append(value.PendingApprovals, approval)
	}
	sort.Slice(value.PendingApprovals, func(i, j int) bool { return value.PendingApprovals[i].ID < value.PendingApprovals[j].ID })
	return value
}

func (r *runningSession) cursor() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nextCursor
}

func (s *Service) lookupSession(id string) (*runningSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.sessions[id]
	return record, ok
}

func (s *Service) closeSession(ctx context.Context, record *runningSession) error {
	record.mu.Lock()
	if record.closed {
		record.mu.Unlock()
		return nil
	}
	record.closed = true
	record.status = "closed"
	record.summary.Status = "closed"
	cancel := record.activeCancel
	done := record.activeDone
	attachments := append([]interactionAttachment(nil), record.attachments...)
	agentSession := record.agent
	record.mu.Unlock()
	var problems []error
	if cancel != nil {
		cancel()
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				problems = append(problems, ctx.Err())
			}
		}
	}
	for _, attachment := range attachments {
		if attachment.instance != nil {
			if err := attachment.instance.Disconnect(ctx); err != nil {
				if attachment.redact != nil {
					err = errors.New(attachment.redact(err.Error()))
				}
				problems = append(problems, err)
			}
		}
		if attachment.release != nil {
			if err := attachment.release(ctx); err != nil {
				problems = append(problems, errors.New("release interaction target material"))
			}
		}
	}
	if agentSession != nil {
		if err := agentSession.Close(ctx); err != nil {
			problems = append(problems, err)
		}
	}
	if s.blockadePool != nil {
		problems = append(problems, s.blockadePool.Close())
	}
	if s.blockadeEngine != nil {
		problems = append(problems, s.blockadeEngine.Close())
	}
	return errors.Join(problems...)
}

func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	records := make([]*runningSession, 0, len(s.sessions))
	for id, record := range s.sessions {
		delete(s.sessions, id)
		records = append(records, record)
	}
	s.mu.Unlock()
	var problems []error
	for _, record := range records {
		if err := s.closeSession(ctx, record); err != nil {
			problems = append(problems, err)
		}
	}
	if s.blockadePool != nil {
		problems = append(problems, s.blockadePool.Close())
	}
	if s.blockadeEngine != nil {
		problems = append(problems, s.blockadeEngine.Close())
	}
	return errors.Join(problems...)
}

var errSessionExists = errors.New("Grimlock session already exists")

func validateCreateSessionRequest(request CreateSessionRequest) error {
	if request.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion must be %q", APIVersion)
	}
	if !userIDPattern.MatchString(request.UserID) {
		return errors.New("userId must be a lowercase DNS-style name")
	}
	if err := request.Agent.Validate(); err != nil {
		return err
	}
	if len(request.Bindings) == 0 || len(request.Bindings) > 64 {
		return errors.New("bindings must contain between 1 and 64 entries")
	}
	seen := make(map[string]struct{}, len(request.Bindings))
	for _, binding := range request.Bindings {
		if !sessionIDPattern.MatchString(binding.InteractionID) {
			return fmt.Errorf("interactionId %q must be a lowercase DNS-style name", binding.InteractionID)
		}
		if _, exists := seen[binding.InteractionID]; exists {
			return fmt.Errorf("interactionId %q is duplicated", binding.InteractionID)
		}
		seen[binding.InteractionID] = struct{}{}
		if strings.TrimSpace(binding.Engine.Adapter) == "" {
			return fmt.Errorf("interaction %q engine.adapter is required", binding.InteractionID)
		}
		if len(binding.Target.Endpoints) == 0 {
			return fmt.Errorf("interaction %q target requires at least one endpoint", binding.InteractionID)
		}
		if binding.Target.APIVersion != "" && binding.Target.APIVersion != engineprovider.TargetAPIVersion {
			return fmt.Errorf("interaction %q target.apiVersion must be %q", binding.InteractionID, engineprovider.TargetAPIVersion)
		}
		if err := validateTarget(binding.Target); err != nil {
			return fmt.Errorf("interaction %q target: %w", binding.InteractionID, err)
		}
		if len(binding.Engine.Options) != 0 {
			var value map[string]any
			if err := json.Unmarshal(binding.Engine.Options, &value); err != nil || value == nil {
				return fmt.Errorf("interaction %q engine.options must be a JSON object", binding.InteractionID)
			}
		}
	}
	return nil
}

func validateTarget(target engineprovider.Target) error {
	if strings.TrimSpace(target.Kind) == "" {
		return errors.New("kind is required")
	}
	if target.APIVersion != "" && target.TargetID == "" {
		return errors.New("targetId is required when apiVersion is supplied")
	}
	if target.TargetID != "" && !targetIDPattern.MatchString(target.TargetID) {
		return errors.New("targetId is invalid")
	}
	seenNames := make(map[string]struct{}, len(target.Endpoints))
	for index, endpoint := range target.Endpoints {
		if !handlePattern.MatchString(endpoint.Name) || strings.TrimSpace(endpoint.Protocol) == "" {
			return fmt.Errorf("endpoint %d name and protocol are required", index)
		}
		if _, exists := seenNames[endpoint.Name]; exists {
			return fmt.Errorf("endpoint %q is duplicated", endpoint.Name)
		}
		seenNames[endpoint.Name] = struct{}{}
		parsed, err := url.Parse(endpoint.URL)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return fmt.Errorf("endpoint %q URL is invalid", endpoint.Name)
		}
		if endpoint.CredentialRef != "" && !profileIDPattern.MatchString(endpoint.CredentialRef) {
			return fmt.Errorf("endpoint %q credentialRef is invalid", endpoint.Name)
		}
		if endpoint.TLSRef != "" && !profileIDPattern.MatchString(endpoint.TLSRef) {
			return fmt.Errorf("endpoint %q tlsRef is invalid", endpoint.Name)
		}
		for name, value := range endpoint.Metadata {
			if !handlePattern.MatchString(name) || strings.ContainsRune(value, '\x00') {
				return fmt.Errorf("endpoint %q metadata is invalid", endpoint.Name)
			}
		}
	}
	for name, value := range target.Handles {
		if !handlePattern.MatchString(name) || value == "" || strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("handle %q is invalid", name)
		}
	}
	for name, value := range target.Metadata {
		if !handlePattern.MatchString(name) || strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("target metadata %q is invalid", name)
		}
	}
	return nil
}

func engineTarget(target engineprovider.Target) orchestrator.EngineTarget {
	endpoints := make([]orchestrator.TargetEndpoint, 0, len(target.Endpoints))
	for _, endpoint := range target.Endpoints {
		endpoints = append(endpoints, orchestrator.TargetEndpoint{
			Name: endpoint.Name, Protocol: endpoint.Protocol, URL: endpoint.URL,
			CredentialRef: endpoint.CredentialRef, TLSRef: endpoint.TLSRef,
			Audience: endpoint.Audience, Metadata: cloneStrings(endpoint.Metadata),
		})
	}
	return orchestrator.EngineTarget{
		APIVersion: target.APIVersion, TargetID: target.TargetID, Kind: target.Kind,
		Endpoints: endpoints, Handles: orchestrator.EngineHandles(cloneStrings(target.Handles)),
		Metadata: cloneStrings(target.Metadata),
	}
}

func bindingPolicy(allowWrites bool) (CapabilityPolicy, func(bridge.Capability) bool) {
	if !allowWrites {
		return ReadOnlyCapabilityPolicy{}, nil
	}
	return CapabilityPolicyFuncs{
		AdvertiseFunc: func(context.Context, CapabilityRequest) bool { return true },
		AuthorizeFunc: func(context.Context, CapabilityRequest) error { return nil },
	}, nil
}

func cleanupAttachments(ctx context.Context, attachments []interactionAttachment) error {
	var problems []error
	for _, attachment := range attachments {
		if attachment.instance != nil {
			if err := attachment.instance.Disconnect(ctx); err != nil {
				problems = append(problems, err)
			}
		}
		if attachment.release != nil {
			if err := attachment.release(ctx); err != nil {
				problems = append(problems, err)
			}
		}
	}
	return errors.Join(problems...)
}

func parseGrimlockCursor(value string) (uint64, error) {
	if value == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errors.New("event cursor must be a non-negative integer")
	}
	return cursor, nil
}

func parseGrimlockLimit(value string) (int, error) {
	if value == "" {
		return 100, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 || limit > grimlockEventLimit {
		return 0, fmt.Errorf("event limit must be between 1 and %d", grimlockEventLimit)
	}
	return limit, nil
}

func cloneEventEnvelope(value EventEnvelope) EventEnvelope {
	value.Event = append(json.RawMessage(nil), value.Event...)
	return value
}

func cloneStrings(value map[string]string) map[string]string {
	result := make(map[string]string, len(value))
	for name, item := range value {
		result[name] = item
	}
	return result
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{Code: code, Message: message})
}
