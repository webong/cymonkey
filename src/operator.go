package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cymonkey/src/internal/engineprovider"
	"cymonkey/src/internal/userscripts"
)

const defaultOperatorURL = "http://127.0.0.1:7395"

func operatorCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("operator requires serve, targets, connect, call, disconnect, observe, userscript, extension, or board")
	}
	switch args[0] {
	case "serve":
		return serveOperator(args[1:], stdout, stderr)
	case "targets", "connect", "call", "disconnect", "observe":
		return operatorClient(args[0], args[1:], stdout)
	case "userscript":
		return operatorUserscriptClient(args[1:], stdout)
	case "extension":
		return operatorExtensionClient(args[1:], stdout)
	case "board":
		return operatorBoardClient(args[1:], stdout)
	default:
		return fmt.Errorf("unknown operator command %q", args[0])
	}
}

func serveOperator(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("cymonkey operator serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	bind := flags.String("bind", "127.0.0.1:7395", "operator HTTP bind address")
	config := flags.String("config", "", "optional Cymonkey observation manifest")
	blockadeURL := flags.String("blockade-url", "", "Blockade HTTP endpoint for image observations")
	store, err := userscripts.DefaultDirectory()
	if err != nil {
		return err
	}
	storeFlag := flags.String("userscripts-store", store, "approved userscript store")
	target := flags.String("target", "", "optionally attach this discovered browser target at startup")
	browser := flags.String("browser", "", "explicit browser name for startup attachment")
	browserBin := flags.String("browser-bin", "", "explicit browser executable path")
	profile := flags.String("profile", "", "explicit browser profile path")
	profileDirectory := flags.String("profile-directory", "", "Chromium profile directory name")
	endpoint := flags.String("endpoint", "", "optional PROTOCOL=URL endpoint for startup attachment")
	instance := flags.String("instance", "", "optional instance ID for startup attachment")
	userscriptsTarget := flags.String("userscripts-target", "", "stable userscript target ID for startup attachment")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("operator serve accepts flags only")
	}
	bindHost, _, err := net.SplitHostPort(*bind)
	if err != nil || !loopbackHost(bindHost) {
		return errors.New("operator serve must bind to loopback; use an authenticated HTTPS proxy for remote access")
	}
	if *blockadeURL != "" {
		parsed, err := url.Parse(*blockadeURL)
		if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
			return errors.New("--blockade-url must be an HTTP URL")
		}
	}
	token := strings.TrimSpace(os.Getenv("CYMONKEY_OPERATOR_TOKEN"))
	registry, err := engineRegistry()
	if err != nil {
		return err
	}
	operator, err := newOperatorServer(registry, token, *storeFlag, *config)
	if err != nil {
		return err
	}
	operator.blockadeURL = *blockadeURL
	defer operator.Close(context.Background())
	if *target != "" || *endpoint != "" || *browser != "" || *userscriptsTarget != "" {
		payload, err := json.Marshal(browserSessionRequest{InstanceID: *instance, TargetID: *target, Browser: *browser, BrowserBin: *browserBin, Profile: *profile, ProfileDirectory: *profileDirectory, Endpoint: *endpoint, UserscriptsTarget: *userscriptsTarget})
		if err != nil {
			return err
		}
		request := httptest.NewRequest(http.MethodPost, "/v1/browser-sessions", bytes.NewReader(payload))
		request.Header.Set("Authorization", "Bearer "+token)
		result := httptest.NewRecorder()
		operator.Routes().ServeHTTP(result, request)
		if result.Code != http.StatusCreated {
			return fmt.Errorf("attach browser: %s", strings.TrimSpace(result.Body.String()))
		}
		if _, err := io.Copy(stdout, result.Body); err != nil {
			return err
		}
	}
	server := &http.Server{Addr: *bind, Handler: operator.Routes(), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Fprintf(stderr, "Cymonkey operator listening on %s\n", *bind)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func operatorClient(command string, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("cymonkey operator "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	base := flags.String("url", defaultOperatorURL, "operator server URL")
	target := flags.String("target", "", "discovered browser target ID")
	browser := flags.String("browser", "", "explicit browser name")
	browserBin := flags.String("browser-bin", "", "explicit browser executable path")
	profile := flags.String("profile", "", "explicit browser profile path")
	profileDirectory := flags.String("profile-directory", "", "Chromium profile directory name")
	endpoint := flags.String("endpoint", "", "browser endpoint as PROTOCOL=URL")
	instance := flags.String("instance", "", "interaction instance ID")
	adapter := flags.String("adapter", "", "interaction adapter name or auto")
	userscriptsTarget := flags.String("userscripts-target", "", "stable userscript target ID")
	name := flags.String("name", "", "semantic action name")
	input := flags.String("input", "{}", "semantic action input JSON")
	approvalID := flags.String("approval-id", "", "approved action receipt")
	prompt := flags.String("prompt", "", "observation prompt")
	fullPage := flags.Bool("full-page", false, "capture the full browser page")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("operator command accepts flags only")
	}
	method, path := http.MethodPost, ""
	var payload any
	switch command {
	case "targets":
		method, path = http.MethodGet, "/v1/browser-targets"
	case "connect":
		path = "/v1/browser-sessions"
		payload = browserSessionRequest{TargetID: *target, Browser: *browser, BrowserBin: *browserBin, Profile: *profile, ProfileDirectory: *profileDirectory, Endpoint: *endpoint, InstanceID: *instance, Adapter: *adapter, UserscriptsTarget: *userscriptsTarget}
	case "call":
		if *instance == "" || *name == "" {
			return errors.New("call requires --instance and --name")
		}
		if !json.Valid([]byte(*input)) {
			return errors.New("--input must be valid JSON")
		}
		path = "/v1/instances/" + url.PathEscape(*instance) + "/call"
		params, _ := json.Marshal(map[string]any{"name": *name, "input": json.RawMessage(*input)})
		payload = map[string]any{"method": "act", "params": json.RawMessage(params), "approvalId": *approvalID}
	case "disconnect":
		if *instance == "" {
			return errors.New("disconnect requires --instance")
		}
		method, path = http.MethodDelete, "/v1/instances/"+url.PathEscape(*instance)
	case "observe":
		if *instance == "" {
			return errors.New("observe requires --instance")
		}
		path = "/v1/observations"
		payload = observationOperatorRequest{InstanceID: *instance, Prompt: *prompt, FullPage: *fullPage, ApprovalID: *approvalID}
	}
	return sendOperatorRequest(*base, method, path, payload, output, 30*time.Second)
}

func operatorUserscriptClient(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("operator userscript requires prepare, install, update, list, describe, enable, disable, or uninstall")
	}
	command := args[0]
	switch command {
	case "prepare", "install", "update", "list", "describe", "enable", "disable", "uninstall":
	default:
		return fmt.Errorf("unknown operator userscript command %q", command)
	}
	flags := flag.NewFlagSet("cymonkey operator userscript "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	base := flags.String("url", defaultOperatorURL, "operator server URL")
	target := flags.String("target", "", "browser or userscript target ID")
	id := flags.String("id", "", "userscript ID")
	name := flags.String("name", "", "userscript name")
	source := flags.String("source", "", "source path on the operator machine")
	revision := flags.String("revision", "", "reviewed source revision")
	var matches, excludes repeatFlag
	flags.Var(&matches, "match", "URL match pattern")
	flags.Var(&excludes, "exclude-match", "URL exclusion pattern")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("operator userscript accepts flags only")
	}
	return sendOperatorRequest(*base, http.MethodPost, "/v1/userscripts/"+command, userscriptOperatorRequest{
		TargetID: *target, ID: *id, Name: *name, SourcePath: *source, Revision: *revision,
		Matches: matches, ExcludeMatches: excludes,
	}, output, 30*time.Second)
}

func operatorExtensionClient(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("cymonkey operator extension", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	base := flags.String("url", defaultOperatorURL, "operator server URL")
	name := flags.String("name", "", "extension action name")
	input := flags.String("input", "{}", "extension action input JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *name == "" || !json.Valid([]byte(*input)) {
		return errors.New("operator extension requires --name and valid --input JSON")
	}
	timeout := 30 * time.Second
	if *name == "extension.install" || *name == "extension.run" {
		timeout = 0
	}
	return sendOperatorRequest(*base, http.MethodPost, "/v1/extensions/actions", extensionOperatorRequest{Name: *name, Input: json.RawMessage(*input)}, output, timeout)
}

func sendOperatorRequest(base, method, path string, payload any, output io.Writer, timeout time.Duration) error {
	token := strings.TrimSpace(os.Getenv("CYMONKEY_OPERATOR_TOKEN"))
	if token == "" {
		return errors.New("CYMONKEY_OPERATOR_TOKEN is required")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return errors.New("--url must be an operator origin")
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !loopbackHost(parsed.Hostname())) {
		return errors.New("operator tokens require HTTPS outside loopback")
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, parsed.String()+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("operator returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	if path == "/v1/extensions/actions" && timeout == 0 {
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 64*1024), 2<<20)
		for scanner.Scan() {
			line := scanner.Bytes()
			if _, err := output.Write(append(append([]byte(nil), line...), '\n')); err != nil {
				return err
			}
			var failure engineprovider.ErrorResponse
			if json.Unmarshal(line, &failure) == nil && failure.Code == "extension_operation_failed" {
				return errors.New(failure.Message)
			}
		}
		return scanner.Err()
	}
	_, err = io.Copy(output, response.Body)
	return err
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
