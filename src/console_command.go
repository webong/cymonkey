package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"board/capture"
	"console"
	"console/bindings"
	"console/httpapi"
	"console/mcp"
	endpointmodels "cymonkey-content-plugin-endpoints/generation"
	"jangolova/contract"
	"jangolova/ui"
	"kit/generationkit"
)

// Configuration belongs to the operator. SDKs never discover credentials,
// select provider profiles, attach new devices or launch MCP server processes.
type consoleConfiguration struct {
	APIVersion         string             `json:"apiVersion"`
	Operator           *consoleConnection `json:"operator,omitempty"`
	Models             []consoleModel     `json:"models,omitempty"`
	Tools              []consoleTool      `json:"tools,omitempty"`
	MCP                []consoleMCP       `json:"mcp,omitempty"`
	MCPProtocolVersion string             `json:"mcpProtocolVersion,omitempty"`
	AllowedOrigins     []string           `json:"allowedOrigins,omitempty"`
}
type consoleConnection struct {
	URL              string `json:"url"`
	TokenEnvironment string `json:"tokenEnvironment,omitempty"`
}
type consoleModel struct {
	ID                 string                  `json:"id"`
	Provider           string                  `json:"provider"`
	Model              string                  `json:"model"`
	Protocol           endpointmodels.Protocol `json:"protocol"`
	BaseURL            string                  `json:"baseURL"`
	APIKeyEnvironment  string                  `json:"apiKeyEnvironment,omitempty"`
	Capabilities       []string                `json:"capabilities"`
	Options            json.RawMessage         `json:"options,omitempty"`
	EmbeddingInputType string                  `json:"embeddingInputType,omitempty"`
}
type consoleTool struct {
	Definition        console.Definition `json:"definition"`
	Kind              string             `json:"kind"`
	ModelID           string             `json:"modelId,omitempty"`
	InstanceID        string             `json:"instanceId,omitempty"`
	SurfaceID         string             `json:"surfaceId,omitempty"`
	Action            string             `json:"action,omitempty"`
	ApprovalID        string             `json:"approvalId,omitempty"`
	SessionID         string             `json:"sessionId,omitempty"`
	ResourceID        string             `json:"resourceId,omitempty"`
	ViewerSurfaceID   string             `json:"viewerSurfaceId,omitempty"`
	MaxCaptureBytes   int                `json:"maxCaptureBytes,omitempty"`
	MaxCapturePixels  int                `json:"maxCapturePixels,omitempty"`
	MaxAudioBytes     int64              `json:"maxAudioBytes,omitempty"`
	ArtifactDirectory string             `json:"artifactDirectory,omitempty"`
	MaxTextBytes      int                `json:"maxTextBytes,omitempty"`
	Voice             string             `json:"voice,omitempty"`
	Format            string             `json:"format,omitempty"`
	Instructions      string             `json:"instructions,omitempty"`
}
type consoleMCP struct {
	Connection      consoleConnection `json:"connection"`
	ProtocolVersion string            `json:"protocolVersion"`
	Prefix          string            `json:"prefix"`
	Tools           []string          `json:"tools"`
}
type configuredGenerator struct {
	client  *endpointmodels.Client
	options json.RawMessage
}

func (m configuredGenerator) Generate(ctx context.Context, r generationkit.GenerationRequest) (generationkit.GenerationResponse, error) {
	if len(r.Options) > 0 {
		return generationkit.GenerationResponse{}, errors.New("native options are fixed by the host binding")
	}
	r.Options = append(json.RawMessage(nil), m.options...)
	return m.client.Generate(ctx, r)
}
func (m configuredGenerator) Stream(ctx context.Context, r generationkit.GenerationRequest, emit func(generationkit.GenerationEvent) error) (generationkit.GenerationResponse, error) {
	if len(r.Options) > 0 {
		return generationkit.GenerationResponse{}, errors.New("native options are fixed by the host binding")
	}
	r.Options = append(json.RawMessage(nil), m.options...)
	return m.client.Stream(ctx, r, emit)
}

type consoleHost struct {
	tools         console.Registry
	models        generationkit.ModelCatalog
	attachments   []*mcp.HTTPTransport
	artifactRoots []*os.Root
	config        consoleConfiguration
}

func (h *consoleHost) close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	for _, connection := range h.attachments {
		err = errors.Join(err, connection.Close(ctx))
	}
	for _, root := range h.artifactRoots {
		err = errors.Join(err, root.Close())
	}
	return err
}
func loadConsole(ctx context.Context, path string) (host *consoleHost, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var config consoleConfiguration
	if err = consoleDecode(io.LimitReader(file, 1<<20+1), &config); err != nil {
		return nil, err
	}
	if config.APIVersion != httpapi.APIVersion || len(config.Models) > 128 || len(config.Tools) > 1024 || len(config.MCP) > 32 {
		return nil, errors.New("invalid Console configuration version or inventory bounds")
	}
	h := &consoleHost{config: config}
	defer func() {
		if err != nil {
			_ = h.close()
		}
	}()
	clients := map[string]*endpointmodels.Client{}
	selected := map[string]consoleModel{}
	for _, model := range config.Models {
		if model.ID == "" || model.Provider == "" || model.Model == "" || clients[model.ID] != nil {
			return nil, errors.New("model bindings require unique IDs, opaque provider IDs and explicit models")
		}
		var key func(context.Context) (string, error)
		if model.APIKeyEnvironment != "" {
			key = endpointmodels.EnvironmentKey(model.APIKeyEnvironment)
		}
		client, e := endpointmodels.New(endpointmodels.Config{Protocol: model.Protocol, BaseURL: model.BaseURL, APIKey: key, EmbeddingInputType: model.EmbeddingInputType})
		if e != nil {
			return nil, e
		}
		if len(model.Options) > 0 {
			var value map[string]any
			if json.Unmarshal(model.Options, &value) != nil || value == nil {
				return nil, errors.New("model options must be an object")
			}
		}
		clients[model.ID] = client
		selected[model.ID] = model
		if e = h.models.Register(generationkit.ModelInfo{ID: model.ID, Provider: model.Provider, Model: model.Model, Capabilities: model.Capabilities}, configuredGenerator{client: client, options: model.Options}); e != nil {
			return nil, e
		}
	}
	var remote *consoleRemote
	if config.Operator != nil {
		remote, err = newConsoleRemote(*config.Operator)
		if err != nil {
			return nil, err
		}
	}
	for _, spec := range config.Tools {
		var tool console.Tool
		var e error
		switch spec.Kind {
		case "generate", "embed", "transcribe", "synthesize":
			client := clients[spec.ModelID]
			model := selected[spec.ModelID]
			if client == nil {
				return nil, fmt.Errorf("tool %q selects an unknown model", spec.Definition.Name)
			}
			switch spec.Kind {
			case "generate":
				tool, e = bindings.Generate(spec.Definition, configuredGenerator{client: client, options: model.Options}, model.Model)
			case "embed":
				tool, e = bindings.Embed(spec.Definition, client, model.Model)
			case "transcribe":
				tool, e = bindings.Transcribe(spec.Definition, client, bindings.TranscriptionOptions{Model: model.Model, MaxAudioBytes: spec.MaxAudioBytes})
			case "synthesize":
				if !filepath.IsAbs(spec.ArtifactDirectory) || spec.MaxAudioBytes < 1 || spec.MaxAudioBytes > 64<<20 {
					return nil, errors.New("synthesis needs an absolute existing artifact directory and audio ceiling")
				}
				root, err := os.OpenRoot(spec.ArtifactDirectory)
				if err != nil {
					return nil, err
				}
				h.artifactRoots = append(h.artifactRoots, root)
				tool, e = bindings.Synthesize(spec.Definition, client, bindings.SpeechOptions{Model: model.Model, Voice: spec.Voice, Format: spec.Format, Instructions: spec.Instructions, MaxTextBytes: spec.MaxTextBytes}, consoleAudioConsumer(root, spec.ArtifactDirectory, spec.Format, spec.MaxAudioBytes))
			}
		case "display", "observe", "see", "verify", "wait", "observed-action":
			if remote == nil || spec.InstanceID == "" || spec.SurfaceID == "" {
				return nil, errors.New("display tools need an operator and fixed instance/surface")
			}
			caller := consoleDisplayCaller{remote: remote, instance: spec.InstanceID, surface: spec.SurfaceID, approval: spec.ApprovalID}
			client := ui.Client{Caller: caller, SurfaceID: spec.SurfaceID}
			switch spec.Kind {
			case "observe":
				tool, e = bindings.Observe(spec.Definition, client, spec.SurfaceID)
			case "see":
				viewer := consoleDisplayCaller{remote: remote, instance: spec.InstanceID, surface: spec.ViewerSurfaceID, approval: spec.ApprovalID}
				tool, e = bindings.See(spec.Definition, client, spec.SurfaceID, capture.Bridge{Caller: viewer, SurfaceID: spec.ViewerSurfaceID, MaxBytes: spec.MaxCaptureBytes, MaxPixels: spec.MaxCapturePixels})
			case "verify":
				tool, e = bindings.Verify(spec.Definition, client, spec.SurfaceID)
			case "wait":
				tool, e = bindings.Wait(spec.Definition, client, spec.SurfaceID)
			case "observed-action":
				tool, e = bindings.ObservedAction(spec.Definition, client, spec.Action)
			case "display":
				tool, e = bindings.Display(spec.Definition, caller, contract.Action{Name: spec.Action})
			}
		case "device":
			if remote == nil || spec.SessionID == "" || spec.Action == "" || spec.ResourceID == "" {
				return nil, errors.New("device tools need a previously granted session, fixed capability and resource")
			}
			tool, e = console.NewTool(spec.Definition, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
				data, err := remote.post(ctx, "/v1/device-sessions/"+url.PathEscape(spec.SessionID)+"/actions", map[string]any{"capability": spec.Action, "resourceId": spec.ResourceID, "input": input})
				if err != nil {
					return nil, err
				}
				var result struct {
					Output json.RawMessage `json:"output"`
				}
				if err = json.Unmarshal(data, &result); err != nil {
					return nil, err
				}
				if len(result.Output) == 0 {
					result.Output = json.RawMessage(`null`)
				}
				return result.Output, nil
			})
		default:
			return nil, fmt.Errorf("unknown Console tool binding %q", spec.Kind)
		}
		if e != nil {
			return nil, e
		}
		if e = h.tools.Register(tool); e != nil {
			return nil, e
		}
	}
	for _, attachment := range config.MCP {
		if len(attachment.Tools) == 0 || len(attachment.Tools) > 1024 {
			return nil, errors.New("MCP attachment needs an explicit tool selection")
		}
		headers, e := consoleHeaders(attachment.Connection.TokenEnvironment)
		if e != nil {
			return nil, e
		}
		transport, e := mcp.NewHTTP(attachment.Connection.URL, nil, headers)
		if e != nil {
			return nil, e
		}
		h.attachments = append(h.attachments, transport)
		client, e := mcp.New(transport)
		if e != nil {
			return nil, e
		}
		if e = client.Initialize(ctx, attachment.ProtocolVersion); e != nil {
			return nil, e
		}
		tools, e := client.Tools(ctx, attachment.Prefix)
		if e != nil {
			return nil, e
		}
		available := map[string]console.Tool{}
		for _, tool := range tools {
			available[tool.Definition().Name] = tool
		}
		for _, name := range attachment.Tools {
			tool, ok := available[name]
			if !ok {
				return nil, fmt.Errorf("selected MCP tool %q is unavailable", name)
			}
			if e = h.tools.Register(tool); e != nil {
				return nil, e
			}
		}
	}
	return h, nil
}

type consoleRemote struct {
	base    string
	headers http.Header
	client  *http.Client
}

func consoleHeaders(name string) (http.Header, error) {
	headers := http.Header{}
	if name != "" {
		token := os.Getenv(name)
		if token == "" {
			return nil, fmt.Errorf("token environment variable %q is unset", name)
		}
		headers.Set("Authorization", "Bearer "+token)
	}
	return headers, nil
}
func newConsoleRemote(connection consoleConnection) (*consoleRemote, error) {
	u, err := url.Parse(connection.URL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("operator connection needs an explicit HTTP(S) root")
	}
	headers, err := consoleHeaders(connection.TokenEnvironment)
	if err != nil {
		return nil, err
	}
	return &consoleRemote{base: strings.TrimRight(connection.URL, "/"), headers: headers, client: &http.Client{Timeout: time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (r *consoleRemote) post(ctx context.Context, path string, value any) (json.RawMessage, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header = r.headers.Clone()
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return nil, fmt.Errorf("operator request returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 || !json.Valid(data) {
		return nil, errors.New("operator response is invalid or exceeds byte ceiling")
	}
	return data, nil
}

type consoleDisplayCaller struct {
	remote                      *consoleRemote
	instance, surface, approval string
}

func (c consoleDisplayCaller) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if method != "act" {
		return nil, errors.New("Console display binding only permits explicit actions")
	}
	var action contract.Action
	if err := json.Unmarshal(params, &action); err != nil {
		return nil, err
	}
	var input map[string]any
	if err := json.Unmarshal(action.Input, &input); err != nil || input == nil {
		return nil, errors.New("display arguments must be an object")
	}
	input["surfaceId"] = c.surface
	action.Input, _ = json.Marshal(input)
	data, err := c.remote.post(ctx, "/v1/display-instances/"+url.PathEscape(c.instance)+"/call", map[string]any{"method": "act", "params": action, "approvalId": c.approval})
	if err != nil {
		return nil, err
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
	}
	if err = json.Unmarshal(data, &reply); err != nil || !json.Valid(reply.Result) {
		return nil, errors.New("operator returned no display result")
	}
	return reply.Result, nil
}

func consoleDecode(reader io.Reader, value any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON value")
	}
	return nil
}
func consoleCommand(args []string, stdout, stderr io.Writer) (err error) {
	if len(args) == 0 {
		return errors.New("console requires tools, models, call, generate, stream, serve, or mcp")
	}
	command := args[0]
	switch command {
	case "tools", "models", "call", "generate", "stream", "serve", "mcp":
	default:
		return fmt.Errorf("unknown console command %q", command)
	}
	flags := flag.NewFlagSet("cymy console "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "explicit Console JSON configuration")
	input := flags.String("input", "-", "JSON request file or stdin")
	bind := flags.String("bind", "", "HTTP listener on loopback")
	tokenEnv := flags.String("token-env", "", "environment variable containing the HTTP bearer token")
	if err = flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *path == "" {
		return errors.New("--config is required; positional arguments are unsupported")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	host, err := loadConsole(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, host.close()) }()
	mcpServer := mcp.Server{Registry: &host.tools, ProtocolVersion: host.config.MCPProtocolVersion, Name: "cymonkey-console", Version: "0.1.0"}
	switch command {
	case "tools":
		return json.NewEncoder(stdout).Encode(host.tools.Definitions())
	case "models":
		return json.NewEncoder(stdout).Encode(host.models.Models())
	case "mcp":
		if host.config.MCPProtocolVersion == "" {
			return errors.New("mcpProtocolVersion is required for MCP serving")
		}
		return mcpServer.ServeIO(ctx, os.Stdin, stdout)
	case "serve":
		address, _, e := net.SplitHostPort(*bind)
		if e != nil || !loopbackHost(address) {
			return errors.New("--bind must specify a loopback listener")
		}
		if *tokenEnv == "" || os.Getenv(*tokenEnv) == "" {
			return errors.New("--token-env must select an existing bearer token")
		}
		token := os.Getenv(*tokenEnv)
		api := httpapi.Server{Tools: &host.tools, Models: &host.models}
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) != 1 {
				http.Error(w, "authorization required", 401)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				allowed := false
				for _, selected := range host.config.AllowedOrigins {
					allowed = allowed || origin == selected
				}
				if !allowed {
					http.Error(w, "origin denied", 403)
					return
				}
			}
			callCtx, cancel := context.WithTimeout(r.Context(), time.Minute)
			defer cancel()
			r = r.WithContext(callCtx)
			if r.URL.Path == "/mcp" {
				mcpServer.ServeHTTP(w, r)
			} else {
				api.ServeHTTP(w, r)
			}
		})
		server := &http.Server{Addr: *bind, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: time.Minute, WriteTimeout: 70 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 32 << 10}
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(shutdown)
			case <-done:
			}
		}()
		fmt.Fprintf(stderr, "Cymonkey Console listening on %s\n", *bind)
		err = server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
	var reader io.Reader = os.Stdin
	if *input != "-" {
		file, e := os.Open(*input)
		if e != nil {
			return e
		}
		defer file.Close()
		reader = file
	}
	reader = io.LimitReader(reader, 1<<20+1)
	if command == "call" {
		var call console.Call
		if err = consoleDecode(reader, &call); err != nil {
			return err
		}
		result, e := host.tools.Invoke(ctx, call)
		if e != nil {
			return e
		}
		return json.NewEncoder(stdout).Encode(result)
	}
	var request generationkit.GenerationRequest
	if err = consoleDecode(reader, &request); err != nil {
		return err
	}
	model := console.ModelTools{Model: &host.models, Tools: &host.tools}
	if command == "stream" {
		_, err = model.Stream(ctx, request, func(event generationkit.GenerationEvent) error { return json.NewEncoder(stdout).Encode(event) })
		return err
	}
	result, err := model.Generate(ctx, request)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}
