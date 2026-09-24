// Package blockadewebllm implements Blockade's browser-local WebLLM inference
// backend. It launches a dedicated Chromium process that is owned by Blockade;
// it never attaches to, reads from, or interacts with a caller's target tab.
package blockadewebllm

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cymonkey/lib/blockade"
)

//go:embed runtime.js
var runtimeJavaScript string

const (
	Kind = "webllm"

	defaultModuleURL         = "https://esm.run/@mlc-ai/web-llm@0.2.84"
	defaultMaxTokens         = 1024
	defaultContextWindowSize = 6144
	maximumRuntimeBodyBytes  = 2 << 20
)

var allowedSettings = map[string]bool{
	"browserExecutable": true,
	"cacheDirectory":    true,
	"contextWindowSize": true,
	"headless":          true,
	"maxTokens":         true,
	"model":             true,
	"moduleURL":         true,
}

type adapterConfig struct {
	model             string
	moduleURL         string
	browserExecutable string
	cacheDirectory    string
	headless          bool
	maxTokens         int
	contextWindowSize int
}

type runtimeState struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

type runtimeConfig struct {
	Model             string `json:"model"`
	ModuleURL         string `json:"moduleURL"`
	MaxTokens         int    `json:"maxTokens"`
	ContextWindowSize int    `json:"contextWindowSize"`
}

type runtimeCall struct {
	ID       string                                 `json:"id"`
	Request  blockade.ProviderAdapterObserveRequest `json:"request"`
	Result   chan runtimeResult                     `json:"-"`
	Canceled chan struct{}                          `json:"-"`
}

type runtimeResult struct {
	Response blockade.ProviderAdapterObserveResponse
	Err      error
}

type runtimeFailure struct {
	Kind    blockade.ProviderAdapterErrorKind `json:"kind"`
	Message string                            `json:"message,omitempty"`
}

type runtimeCompletion struct {
	ID     string                                   `json:"id"`
	Result *blockade.ProviderAdapterObserveResponse `json:"result,omitempty"`
	Error  *runtimeFailure                          `json:"error,omitempty"`
}

type Adapter struct {
	config adapterConfig
	token  string

	cancel           context.CancelFunc
	server           *http.Server
	listener         net.Listener
	browser          *exec.Cmd
	browserDone      chan struct{}
	profile          string
	temporaryProfile bool
	done             chan struct{}
	closeOnce        sync.Once
	closeErr         error
	nextID           atomic.Uint64

	mu           sync.Mutex
	state        string
	detail       string
	stateChanged chan struct{}
	pending      map[string]*runtimeCall
	queue        chan *runtimeCall
}

// Register adds the WebLLM adapter kind to a Blockade provider registry.
func Register(registry *blockade.ProviderAdapterRegistry) error {
	return registry.Register(Kind, New)
}

// New starts a dedicated browser-local WebLLM runtime.
func New(ctx context.Context, providerRuntime blockade.ProviderAdapterRuntime) (blockade.ProviderAdapter, error) {
	config, err := parseConfig(providerRuntime.Settings)
	if err != nil {
		return nil, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidRequest, err)
	}
	browserExecutable, err := resolveBrowserExecutable(config.browserExecutable)
	if err != nil {
		return nil, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorUnavailable, err)
	}
	token, err := randomToken()
	if err != nil {
		return nil, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorUnavailable, err)
	}
	profile, temporaryProfile, err := prepareProfileDirectory(config.cacheDirectory, providerRuntime.ID)
	if err != nil {
		return nil, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorUnavailable, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if temporaryProfile {
			_ = os.RemoveAll(profile)
		}
		return nil, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorUnavailable, fmt.Errorf("listen for WebLLM runtime: %w", err))
	}
	adapterCtx, cancel := context.WithCancel(ctx)
	a := &Adapter{
		config: config, token: token,
		cancel: cancel, listener: listener, profile: profile, temporaryProfile: temporaryProfile,
		done: make(chan struct{}), state: "starting", stateChanged: make(chan struct{}),
		pending: make(map[string]*runtimeCall), queue: make(chan *runtimeCall),
	}
	a.server = &http.Server{Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serveErr := a.server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			a.setState("error", "WebLLM runtime server stopped")
		}
	}()

	runtimeURL := "http://" + listener.Addr().String() + "/?token=" + url.QueryEscape(token)
	arguments := []string{
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--disable-component-update",
		"--enable-unsafe-webgpu",
		"--window-size=1280,1024",
	}
	if config.headless {
		arguments = append(arguments, "--headless=new")
	}
	arguments = append(arguments, runtimeURL)
	a.browser = exec.CommandContext(adapterCtx, browserExecutable, arguments...)
	a.browserDone = make(chan struct{})
	a.browser.Stdout = io.Discard
	a.browser.Stderr = io.Discard
	if err := a.browser.Start(); err != nil {
		close(a.browserDone)
		_ = a.Close()
		return nil, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorUnavailable, fmt.Errorf("start WebLLM browser: %w", err))
	}
	go func() {
		defer close(a.browserDone)
		err := a.browser.Wait()
		select {
		case <-a.done:
			return
		default:
		}
		if err != nil {
			a.setState("error", "WebLLM browser exited unexpectedly")
		} else {
			a.setState("error", "WebLLM browser stopped")
		}
	}()
	return a, nil
}

func parseConfig(settings map[string]string) (adapterConfig, error) {
	for key := range settings {
		if !allowedSettings[key] {
			return adapterConfig{}, fmt.Errorf("WebLLM setting %q is not supported", key)
		}
	}
	config := adapterConfig{
		model: strings.TrimSpace(settings["model"]), moduleURL: strings.TrimSpace(settings["moduleURL"]),
		browserExecutable: strings.TrimSpace(settings["browserExecutable"]), cacheDirectory: strings.TrimSpace(settings["cacheDirectory"]), headless: true,
		maxTokens: defaultMaxTokens, contextWindowSize: defaultContextWindowSize,
	}
	if config.model == "" {
		return adapterConfig{}, errors.New("WebLLM setting model is required")
	}
	if config.moduleURL == "" {
		config.moduleURL = defaultModuleURL
	}
	moduleURL, err := url.Parse(config.moduleURL)
	if err != nil || moduleURL.Scheme != "https" || moduleURL.Host == "" {
		return adapterConfig{}, errors.New("WebLLM setting moduleURL must be an absolute HTTPS URL")
	}
	if value := strings.TrimSpace(settings["headless"]); value != "" {
		config.headless, err = strconv.ParseBool(value)
		if err != nil {
			return adapterConfig{}, errors.New("WebLLM setting headless must be true or false")
		}
	}
	if value := strings.TrimSpace(settings["maxTokens"]); value != "" {
		config.maxTokens, err = positiveIntegerSetting("maxTokens", value, 4096)
		if err != nil {
			return adapterConfig{}, err
		}
	}
	if value := strings.TrimSpace(settings["contextWindowSize"]); value != "" {
		config.contextWindowSize, err = positiveIntegerSetting("contextWindowSize", value, 32768)
		if err != nil {
			return adapterConfig{}, err
		}
	}
	return config, nil
}

func prepareProfileDirectory(configured, adapterID string) (string, bool, error) {
	if configured == "" {
		cacheRoot, err := os.UserCacheDir()
		if err == nil {
			configured = filepath.Join(cacheRoot, "blockade", "webllm", safePathSegment(adapterID))
		} else {
			temporary, tempErr := os.MkdirTemp("", "blockade-webllm-profile-")
			if tempErr != nil {
				return "", false, fmt.Errorf("create WebLLM browser profile: %w", tempErr)
			}
			return temporary, true, nil
		}
	}
	absolute, err := filepath.Abs(configured)
	if err != nil {
		return "", false, fmt.Errorf("resolve WebLLM cache directory %q: %w", configured, err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", false, fmt.Errorf("create WebLLM cache directory %q: %w", absolute, err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return "", false, fmt.Errorf("WebLLM cache directory %q is not a directory", absolute)
	}
	return absolute, false, nil
}

func safePathSegment(value string) string {
	value = strings.TrimSpace(value)
	var result strings.Builder
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9', character == '-', character == '_':
			result.WriteRune(character)
		default:
			result.WriteByte('-')
		}
	}
	if result.Len() == 0 {
		return "default"
	}
	return result.String()
}

func positiveIntegerSetting(name, value string, maximum int) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 || parsed > maximum {
		return 0, fmt.Errorf("WebLLM setting %s must be an integer in [1,%d]", name, maximum)
	}
	return parsed, nil
}

func resolveBrowserExecutable(configured string) (string, error) {
	if configured != "" {
		info, err := os.Stat(configured)
		if err != nil {
			return "", fmt.Errorf("WebLLM browser executable %q: %w", configured, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("WebLLM browser executable %q is a directory", configured)
		}
		return configured, nil
	}
	candidates := []string{"google-chrome", "chromium", "chromium-browser", "chrome"}
	if runtime.GOOS == "darwin" {
		candidates = append([]string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			filepath.Join(os.Getenv("HOME"), "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
		}, candidates...)
	}
	for _, candidate := range candidates {
		if filepath.IsAbs(candidate) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
			continue
		}
		if found, err := exec.LookPath(candidate); err == nil {
			return found, nil
		}
	}
	return "", errors.New("no Chromium browser found; configure providerAdapters[].settings.browserExecutable")
}

func randomToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("create WebLLM runtime token: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func (a *Adapter) Observe(ctx context.Context, request blockade.ProviderAdapterObserveRequest) (blockade.ProviderAdapterObserveResponse, error) {
	if err := a.waitReady(ctx); err != nil {
		return blockade.ProviderAdapterObserveResponse{}, err
	}
	id := fmt.Sprintf("webllm-%d", a.nextID.Add(1))
	call := &runtimeCall{ID: id, Request: request, Result: make(chan runtimeResult, 1), Canceled: make(chan struct{})}
	a.mu.Lock()
	a.pending[id] = call
	a.mu.Unlock()
	defer a.removePending(id)
	select {
	case a.queue <- call:
	case <-ctx.Done():
		return blockade.ProviderAdapterObserveResponse{}, ctx.Err()
	case <-a.done:
		return blockade.ProviderAdapterObserveResponse{}, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorUnavailable, errors.New("WebLLM runtime is closed"))
	}
	select {
	case result := <-call.Result:
		return result.Response, result.Err
	case <-ctx.Done():
		return blockade.ProviderAdapterObserveResponse{}, ctx.Err()
	case <-a.done:
		return blockade.ProviderAdapterObserveResponse{}, blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorUnavailable, errors.New("WebLLM runtime is closed"))
	}
}

func (a *Adapter) Capabilities(ctx context.Context) (blockade.ProviderAdapterCapabilities, error) {
	if err := ctx.Err(); err != nil {
		return blockade.ProviderAdapterCapabilities{}, err
	}
	return blockade.ProviderAdapterCapabilities{
		APIVersion:   blockade.ProviderAdapterAPIVersion,
		Capabilities: []string{blockade.CapabilityVisionLanguage},
	}, nil
}

func (a *Adapter) Health(ctx context.Context) (blockade.ProviderAdapterHealth, error) {
	if err := ctx.Err(); err != nil {
		return blockade.ProviderAdapterHealth{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return blockade.ProviderAdapterHealth{
		APIVersion: blockade.ProviderAdapterAPIVersion,
		Ready:      a.state == "ready", Detail: a.detail,
	}, nil
}

func (a *Adapter) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		close(a.done)
		a.cancel()
		a.setState("closed", "WebLLM runtime is closed")
		if a.browser != nil && a.browser.Process != nil {
			_ = a.browser.Process.Kill()
		}
		if a.browserDone != nil {
			select {
			case <-a.browserDone:
			case <-time.After(5 * time.Second):
				if a.closeErr == nil {
					a.closeErr = errors.New("timed out stopping WebLLM browser")
				}
			}
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if a.server != nil {
			a.closeErr = a.server.Shutdown(shutdownCtx)
		}
		if a.temporaryProfile && a.profile != "" {
			if err := os.RemoveAll(a.profile); a.closeErr == nil {
				a.closeErr = err
			}
		}
	})
	return a.closeErr
}

func (a *Adapter) waitReady(ctx context.Context) error {
	for {
		a.mu.Lock()
		state, detail, changed := a.state, a.detail, a.stateChanged
		a.mu.Unlock()
		switch state {
		case "ready":
			return nil
		case "error", "closed":
			return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorUnavailable, errors.New(detail))
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		case <-a.done:
			return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorUnavailable, errors.New("WebLLM runtime is closed"))
		}
	}
}

func (a *Adapter) setState(state, detail string) {
	detail = strings.TrimSpace(detail)
	if len(detail) > 512 {
		detail = detail[:512]
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == "closed" && state != "closed" {
		return
	}
	a.state, a.detail = state, detail
	close(a.stateChanged)
	a.stateChanged = make(chan struct{})
}

func (a *Adapter) removePending(id string) {
	a.mu.Lock()
	call := a.pending[id]
	if call != nil {
		delete(a.pending, id)
	}
	a.mu.Unlock()
	if call != nil {
		close(call.Canceled)
	}
}

func (a *Adapter) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.authorize(a.handleIndex))
	mux.HandleFunc("GET /runtime.js", a.authorize(a.handleRuntimeJS))
	mux.HandleFunc("GET /config", a.authorize(a.handleConfig))
	mux.HandleFunc("POST /status", a.authorize(a.handleStatus))
	mux.HandleFunc("GET /next", a.authorize(a.handleNext))
	mux.HandleFunc("GET /cancel", a.authorize(a.handleCancel))
	mux.HandleFunc("POST /complete", a.authorize(a.handleComplete))
	return mux
}

func (a *Adapter) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != a.token {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next(w, r)
	}
}

func (a *Adapter) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>Blockade WebLLM</title><script type="module" src="/runtime.js?token=`+url.QueryEscape(a.token)+`"></script>`)
}

func (a *Adapter) handleRuntimeJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = io.WriteString(w, runtimeJavaScript)
}

func (a *Adapter) handleConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(runtimeConfig{
		Model: a.config.model, ModuleURL: a.config.moduleURL,
		MaxTokens: a.config.maxTokens, ContextWindowSize: a.config.contextWindowSize,
	})
}

func (a *Adapter) handleStatus(w http.ResponseWriter, r *http.Request) {
	var status runtimeState
	if err := decodeRuntimeJSON(r, &status); err != nil {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	switch status.State {
	case "loading", "ready", "error":
		a.setState(status.State, status.Detail)
	default:
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *Adapter) handleNext(w http.ResponseWriter, r *http.Request) {
	select {
	case call := <-a.queue:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(call)
	case <-r.Context().Done():
	case <-a.done:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *Adapter) handleCancel(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	call := a.pending[r.URL.Query().Get("id")]
	a.mu.Unlock()
	if call == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	select {
	case <-call.Canceled:
		w.WriteHeader(http.StatusNoContent)
	case <-r.Context().Done():
	case <-a.done:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *Adapter) handleComplete(w http.ResponseWriter, r *http.Request) {
	var completion runtimeCompletion
	if err := decodeRuntimeJSON(r, &completion); err != nil || completion.ID == "" || (completion.Result == nil) == (completion.Error == nil) {
		http.Error(w, "invalid completion", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	call := a.pending[completion.ID]
	if call != nil {
		delete(a.pending, completion.ID)
	}
	a.mu.Unlock()
	if call == nil {
		http.Error(w, "unknown completion", http.StatusNotFound)
		return
	}
	result := runtimeResult{}
	if completion.Error != nil {
		kind := completion.Error.Kind
		if !validFailureKind(kind) {
			kind = blockade.ProviderAdapterErrorUnavailable
		}
		result.Err = blockade.NewProviderAdapterError(kind, errors.New(completion.Error.Message))
	} else {
		result.Response = *completion.Result
	}
	select {
	case call.Result <- result:
	default:
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeRuntimeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, maximumRuntimeBodyBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON document")
	}
	return nil
}

func validFailureKind(kind blockade.ProviderAdapterErrorKind) bool {
	switch kind {
	case blockade.ProviderAdapterErrorAuthentication,
		blockade.ProviderAdapterErrorRateLimit,
		blockade.ProviderAdapterErrorTimeout,
		blockade.ProviderAdapterErrorCanceled,
		blockade.ProviderAdapterErrorUnavailable,
		blockade.ProviderAdapterErrorInvalidRequest,
		blockade.ProviderAdapterErrorInvalidResponse:
		return true
	default:
		return false
	}
}
