// Package effecthousecymonkey provides a guarded, file-backed bridge for an
// already-open Effect House project. Effect House owns project import, scene
// attachment, preview, and publishing; this module only handles explicitly
// registered APJS TypeScript component sources.
package effecthousecymonkey

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	jangolova "cymonkey/lib/jangolova"
	contract "cymonkey/lib/jangolova/contract"
	"cymonkey/lib/jangolova/sdk"
)

const (
	Runtime  = "effect-house"
	Protocol = "local-project"

	ActionProjectDescribe = "project.describe"
	ActionScriptList      = "script.list"
	ActionScriptRead      = "script.read"
	ActionScriptReplace   = "script.replace"
	ActionScriptValidate  = "script.validate"

	methodHealth = "health"
	maxSource    = 1 << 20
)

var scriptName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ScriptResource is a project-owner declaration. Only these paths are visible
// to the module. Path must be a relative .ts path within the target root.
type ScriptResource struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Writable bool   `json:"writable,omitempty"`
}

// Options are carried by EngineSpec.Options. Setting AllowWrite alone is not
// sufficient: the individual ScriptResource must also be writable.
type Options struct {
	RegisteredScripts []ScriptResource `json:"registeredScripts"`
	AllowWrite        bool             `json:"allowWrite,omitempty"`
	MaxSourceBytes    int              `json:"maxSourceBytes,omitempty"`
}

type Backend struct{}

type instance struct {
	host    sdk.Host
	target  sdk.EngineTarget
	root    string
	options Options

	mu        sync.Mutex
	closed    bool
	events    []event
	next      uint64
	lifecycle chan sdk.EngineEvent
	closeOnce sync.Once
}

type event struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurredAt"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type scriptReadInput struct {
	ScriptID string `json:"scriptId"`
}

type scriptReplaceInput struct {
	ScriptID       string `json:"scriptId"`
	ExpectedSHA256 string `json:"expectedSha256"`
	Source         string `json:"source"`
}

type scriptCandidateInput struct {
	ScriptID string `json:"scriptId"`
	Source   string `json:"source"`
}

var (
	_ jangolova.Backend            = Backend{}
	_ sdk.EngineInstance           = (*instance)(nil)
	_ sdk.EngineHealthProvider     = (*instance)(nil)
	_ sdk.EngineCapabilityProvider = (*instance)(nil)
	_ sdk.EngineEventSource        = (*instance)(nil)
	_ sdk.Caller                   = (*instance)(nil)
)

func (Backend) Name() jangolova.BackendName {
	return jangolova.BackendName("effect-house-project-files")
}
func (Backend) Domains() []contract.Domain { return []contract.Domain{contract.DomainRender} }

func (Backend) Compatible(target sdk.EngineTarget) bool {
	if target.Kind != Runtime {
		return false
	}
	_, ok := target.Endpoint(Protocol)
	return ok
}

func (Backend) Connect(ctx context.Context, spec sdk.EngineSpec, target sdk.EngineTarget, config jangolova.Options) (sdk.EngineInstance, error) {
	if !(Backend{}).Compatible(target) {
		return nil, fmt.Errorf("Effect House requires target.kind %q with a caller-owned %s endpoint", Runtime, Protocol)
	}
	endpoint, _ := target.Endpoint(Protocol)
	if err := config.Host.Validate(endpoint); err != nil {
		return nil, err
	}
	root, err := validateProjectRoot(endpoint.URL)
	if err != nil {
		return nil, err
	}
	options, err := decodeOptions(spec.Options)
	if err != nil {
		return nil, err
	}
	if err := validateResources(root, options); err != nil {
		return nil, err
	}
	running := &instance{host: config.Host, target: target, root: root, options: options, lifecycle: make(chan sdk.EngineEvent, 1)}
	if missing := missingCapabilities(spec.RequiredCapabilities, running.EngineCapabilities()); len(missing) != 0 {
		return nil, fmt.Errorf("Effect House project is missing required capabilities: %s", strings.Join(missing, ", "))
	}
	running.appendEvent("effect-house.project.attached", map[string]any{"registeredScripts": len(options.RegisteredScripts)})
	return running, nil
}

func (i *instance) Disconnect(context.Context) error {
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return nil
	}
	i.closed = true
	i.mu.Unlock()
	i.closeOnce.Do(func() {
		i.lifecycle <- sdk.EngineEvent{Type: "interaction.disconnected", Status: sdk.EngineHealthStopped, OccurredAt: time.Now().UTC()}
		close(i.lifecycle)
	})
	return nil
}

func (i *instance) Authorize(_ context.Context, request sdk.AuthorizeRequest) (sdk.AuthorizeDecision, error) {
	switch request.Action {
	case ActionProjectDescribe, ActionScriptList, ActionScriptRead, ActionScriptValidate:
		return sdk.AuthorizeDecision{Authorized: true}, nil
	case ActionScriptReplace:
		input, err := decodeReplaceInput(request.Input)
		if err != nil {
			return sdk.AuthorizeDecision{Reason: err.Error()}, nil
		}
		resource, ok := i.resource(input.ScriptID)
		if !ok {
			return sdk.AuthorizeDecision{Reason: "script is not registered"}, nil
		}
		if !i.options.AllowWrite || !resource.Writable {
			return sdk.AuthorizeDecision{Reason: "script replacement is not allowed"}, nil
		}
		if err := validateAPJSComponent(input.Source, i.limit()); err != nil {
			return sdk.AuthorizeDecision{Reason: err.Error()}, nil
		}
		return sdk.AuthorizeDecision{Authorized: true}, nil
	default:
		return sdk.AuthorizeDecision{Reason: "Effect House action is not exposed by this bridge"}, nil
	}
}

func (i *instance) EngineHealth(ctx context.Context) sdk.EngineHealth {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return sdk.EngineHealth{Status: sdk.EngineHealthStopped, ObservedAt: time.Now().UTC()}
	}
	if err := i.host.Validate(i.endpoint()); err != nil {
		return sdk.EngineHealth{Status: sdk.EngineHealthUnhealthy, Message: err.Error(), ObservedAt: time.Now().UTC()}
	}
	for _, resource := range i.options.RegisteredScripts {
		if _, err := i.read(resource); err != nil {
			return sdk.EngineHealth{Status: sdk.EngineHealthUnhealthy, Message: err.Error(), ObservedAt: time.Now().UTC()}
		}
	}
	return sdk.EngineHealth{Status: sdk.EngineHealthHealthy, ObservedAt: time.Now().UTC()}
}

func (i *instance) EngineCapabilities() []string {
	values := []string{"act", "capabilities", "describe", "events", "health", "target.effect-house-project", ActionProjectDescribe, ActionScriptList, ActionScriptRead, ActionScriptValidate}
	if i.options.AllowWrite && hasWritable(i.options.RegisteredScripts) {
		values = append(values, ActionScriptReplace)
	}
	sort.Strings(values)
	return values
}

func (i *instance) EngineEvents() <-chan sdk.EngineEvent { return i.lifecycle }

func (i *instance) Call(ctx context.Context, method string, raw json.RawMessage) (json.RawMessage, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil, errors.New("Effect House project interaction is disconnected")
	}
	if err := i.host.Validate(i.endpoint()); err != nil {
		return nil, err
	}
	switch method {
	case sdk.MethodHello:
		return json.Marshal(contract.Hello{ProtocolVersion: contract.ProtocolVersion, Implementation: contract.Implementation{Name: "jangolova-effect-house", Version: "0.1.0"}, Domains: []contract.Domain{contract.DomainRender}, Runtimes: []string{Runtime}, Drivers: []contract.Driver{contract.Driver("effect-house-project-files")}, Features: []string{"caller-owned-project", "explicit-script-registration", "hash-precondition", "rollback"}})
	case sdk.MethodCapabilities:
		return json.Marshal(i.capabilities())
	case sdk.MethodDescribe:
		return i.describe()
	case sdk.MethodAct:
		return i.act(ctx, raw)
	case sdk.MethodEvents:
		return i.eventsSince(raw)
	case methodHealth:
		health := i.healthLocked()
		return json.Marshal(map[string]any{"status": health.Status, "message": health.Message, "observedAt": health.ObservedAt})
	default:
		return nil, fmt.Errorf("unsupported Effect House interaction method %q", method)
	}
}

func (i *instance) capabilities() []contract.Capability {
	result := []contract.Capability{
		{Name: ActionProjectDescribe, Description: "Describe only the caller-registered APJS TypeScript script resources.", Domain: contract.DomainRender, Runtime: Runtime, Driver: contract.Driver("effect-house-project-files"), Support: contract.SupportMapped, Lifetime: contract.LifetimeAttachment, Persistence: contract.PersistenceSession, Effect: sdk.EffectRead, ResourceKinds: []string{"project", "script"}, InputSchema: objectSchema()},
		{Name: ActionScriptList, Description: "List caller-registered Effect House APJS scripts without source contents.", Domain: contract.DomainRender, Runtime: Runtime, Driver: contract.Driver("effect-house-project-files"), Support: contract.SupportMapped, Lifetime: contract.LifetimeAttachment, Persistence: contract.PersistenceSession, Effect: sdk.EffectRead, ResourceKinds: []string{"script"}, InputSchema: objectSchema()},
		{Name: ActionScriptRead, Description: "Read one caller-registered Effect House APJS script.", Domain: contract.DomainRender, Runtime: Runtime, Driver: contract.Driver("effect-house-project-files"), Support: contract.SupportMapped, Lifetime: contract.LifetimeAttachment, Persistence: contract.PersistenceSession, Effect: sdk.EffectRead, ResourceKinds: []string{"script"}, InputSchema: scriptIDSchema()},
		{Name: ActionScriptValidate, Description: "Validate the minimum documented APJS component shape without changing a file.", Domain: contract.DomainRender, Runtime: Runtime, Driver: contract.Driver("effect-house-project-files"), Support: contract.SupportMapped, Lifetime: contract.LifetimeCall, Persistence: contract.PersistenceEphemeral, Effect: sdk.EffectRead, ResourceKinds: []string{"script"}, InputSchema: candidateSchema()},
	}
	if i.options.AllowWrite && hasWritable(i.options.RegisteredScripts) {
		result = append(result, contract.Capability{Name: ActionScriptReplace, Description: "Atomically replace one allowlisted APJS component only when its current SHA-256 matches expectedSha256; returns rollback content.", Domain: contract.DomainRender, Runtime: Runtime, Driver: contract.Driver("effect-house-project-files"), Support: contract.SupportMapped, Lifetime: contract.LifetimeAttachment, Persistence: contract.PersistenceSession, Effect: sdk.EffectWrite, ResourceKinds: []string{"script"}, InputSchema: replaceSchema()})
	}
	return result
}

func (i *instance) describe() (json.RawMessage, error) {
	projectID := strings.TrimSpace(i.target.TargetID)
	if projectID == "" {
		projectID = "effect-house-project"
	}
	surfaces := []contract.Surface{{ID: projectID, Domain: contract.DomainRender, Runtime: Runtime, Kind: "project", Label: "Attached Effect House project", Properties: json.RawMessage(`{"registered":true}`)}}
	for _, resource := range i.options.RegisteredScripts {
		source, err := i.read(resource)
		if err != nil {
			return nil, err
		}
		properties, _ := json.Marshal(map[string]any{"relativePath": resource.Path, "writable": resource.Writable, "sha256": digest(source), "bytes": len(source)})
		surfaces = append(surfaces, contract.Surface{ID: projectID + ":script:" + resource.ID, Domain: contract.DomainRender, Runtime: Runtime, Kind: "script", Label: resource.ID, Properties: properties})
	}
	return json.Marshal(contract.Description{Revision: fmt.Sprintf("sources:%d", time.Now().UTC().UnixNano()), Surfaces: surfaces, Augmentations: []contract.AugmentationSummary{}})
}

func (i *instance) act(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var action struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &action); err != nil {
		return nil, fmt.Errorf("decode Effect House action: %w", err)
	}
	if len(action.Input) == 0 {
		action.Input = json.RawMessage(`{}`)
	}
	decision, err := i.Authorize(ctx, sdk.AuthorizeRequest{TargetID: i.target.TargetID, Action: action.Name, Input: action.Input})
	if err != nil {
		return nil, err
	}
	if !decision.Authorized {
		return nil, errors.New(decision.Reason)
	}
	switch action.Name {
	case ActionProjectDescribe:
		result, err := i.describe()
		if err == nil {
			i.appendEvent("effect-house.project.described", map[string]any{})
		}
		return result, err
	case ActionScriptList:
		return i.list()
	case ActionScriptRead:
		return i.scriptRead(action.Input)
	case ActionScriptValidate:
		return i.scriptValidate(action.Input)
	case ActionScriptReplace:
		return i.scriptReplace(action.Input)
	default:
		return nil, fmt.Errorf("Effect House action %q is not exposed by this bridge", action.Name)
	}
}

func (i *instance) list() (json.RawMessage, error) {
	result := make([]map[string]any, 0, len(i.options.RegisteredScripts))
	for _, resource := range i.options.RegisteredScripts {
		source, err := i.read(resource)
		if err != nil {
			return nil, err
		}
		result = append(result, map[string]any{"id": resource.ID, "relativePath": resource.Path, "writable": resource.Writable, "sha256": digest(source), "bytes": len(source)})
	}
	i.appendEvent("effect-house.script.listed", map[string]any{})
	return json.Marshal(map[string]any{"scripts": result})
}

func (i *instance) scriptRead(raw json.RawMessage) (json.RawMessage, error) {
	input, err := decodeScriptID(raw)
	if err != nil {
		return nil, err
	}
	resource, ok := i.resource(input.ScriptID)
	if !ok {
		return nil, errors.New("script is not registered")
	}
	source, err := i.read(resource)
	if err != nil {
		return nil, err
	}
	i.appendEvent("effect-house.script.read", map[string]any{"scriptId": resource.ID})
	return json.Marshal(map[string]any{"id": resource.ID, "relativePath": resource.Path, "sha256": digest(source), "source": string(source)})
}

func (i *instance) scriptValidate(raw json.RawMessage) (json.RawMessage, error) {
	input, err := decodeCandidateInput(raw)
	if err != nil {
		return nil, err
	}
	if _, ok := i.resource(input.ScriptID); !ok {
		return nil, errors.New("script is not registered")
	}
	err = validateAPJSComponent(input.Source, i.limit())
	result := map[string]any{"valid": err == nil}
	if err != nil {
		result["reason"] = err.Error()
	}
	return json.Marshal(result)
}

func (i *instance) scriptReplace(raw json.RawMessage) (json.RawMessage, error) {
	input, err := decodeReplaceInput(raw)
	if err != nil {
		return nil, err
	}
	resource, ok := i.resource(input.ScriptID)
	if !ok {
		return nil, errors.New("script is not registered")
	}
	previous, err := i.read(resource)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(digest(previous), input.ExpectedSHA256) {
		return nil, errors.New("script source changed; expectedSha256 does not match")
	}
	if err := writeAtomic(i.path(resource), []byte(input.Source)); err != nil {
		return nil, err
	}
	i.appendEvent("effect-house.script.replaced", map[string]any{"scriptId": resource.ID})
	return json.Marshal(map[string]any{"changed": true, "id": resource.ID, "sha256": digest([]byte(input.Source)), "rollback": map[string]any{"name": ActionScriptReplace, "input": map[string]any{"scriptId": resource.ID, "expectedSha256": digest([]byte(input.Source)), "source": string(previous)}}})
}

func (i *instance) resource(id string) (ScriptResource, bool) {
	for _, resource := range i.options.RegisteredScripts {
		if resource.ID == id {
			return resource, true
		}
	}
	return ScriptResource{}, false
}

func (i *instance) endpoint() sdk.TargetEndpoint {
	endpoint, _ := i.target.Endpoint(Protocol)
	return endpoint
}
func (i *instance) limit() int {
	if i.options.MaxSourceBytes > 0 {
		return i.options.MaxSourceBytes
	}
	return maxSource
}

func (i *instance) path(resource ScriptResource) string {
	return filepath.Join(i.root, filepath.FromSlash(resource.Path))
}

func (i *instance) read(resource ScriptResource) ([]byte, error) {
	path := i.path(resource)
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registered Effect House script %q: %w", resource.ID, err)
	}
	if len(source) > i.limit() {
		return nil, fmt.Errorf("registered Effect House script %q exceeds source limit", resource.ID)
	}
	return source, nil
}

func (i *instance) healthLocked() sdk.EngineHealth {
	if i.closed {
		return sdk.EngineHealth{Status: sdk.EngineHealthStopped, ObservedAt: time.Now().UTC()}
	}
	for _, resource := range i.options.RegisteredScripts {
		if _, err := i.read(resource); err != nil {
			return sdk.EngineHealth{Status: sdk.EngineHealthUnhealthy, Message: err.Error(), ObservedAt: time.Now().UTC()}
		}
	}
	return sdk.EngineHealth{Status: sdk.EngineHealthHealthy, ObservedAt: time.Now().UTC()}
}

func (i *instance) appendEvent(typ string, data any) {
	i.next++
	raw, _ := json.Marshal(data)
	i.events = append(i.events, event{ID: fmt.Sprint(i.next), Type: typ, OccurredAt: time.Now().UTC(), Data: raw})
	if len(i.events) > 256 {
		i.events = append([]event(nil), i.events[len(i.events)-256:]...)
	}
}

func (i *instance) eventsSince(raw json.RawMessage) (json.RawMessage, error) {
	var query struct {
		After string   `json:"after"`
		Types []string `json:"types"`
		Limit int      `json:"limit"`
	}
	if len(raw) != 0 && json.Unmarshal(raw, &query) != nil {
		return nil, errors.New("invalid Effect House event query")
	}
	limit := query.Limit
	if limit <= 0 || limit > 256 {
		limit = 100
	}
	allowed := map[string]struct{}{}
	for _, typ := range query.Types {
		allowed[typ] = struct{}{}
	}
	selected := make([]event, 0, limit)
	for _, value := range i.events {
		if compareCursor(value.ID, query.After) <= 0 {
			continue
		}
		if len(allowed) != 0 {
			if _, ok := allowed[value.Type]; !ok {
				continue
			}
		}
		selected = append(selected, value)
		if len(selected) == limit {
			break
		}
	}
	return json.Marshal(struct {
		Events []event `json:"events"`
		Cursor string  `json:"cursor"`
	}{Events: selected, Cursor: fmt.Sprint(i.next)})
}

func decodeOptions(raw json.RawMessage) (Options, error) {
	var options Options
	if len(bytes.TrimSpace(raw)) == 0 {
		return options, errors.New("Effect House options require registeredScripts")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&options); err != nil {
		return options, fmt.Errorf("decode Effect House options: %w", err)
	}
	if len(options.RegisteredScripts) == 0 {
		return options, errors.New("Effect House options require registeredScripts")
	}
	if options.MaxSourceBytes < 0 || options.MaxSourceBytes > maxSource {
		return options, fmt.Errorf("maxSourceBytes must be between 1 and %d", maxSource)
	}
	seenIDs, seenPaths := map[string]struct{}{}, map[string]struct{}{}
	for _, resource := range options.RegisteredScripts {
		if strings.TrimSpace(resource.ID) == "" {
			return options, errors.New("registered Effect House script requires id")
		}
		if _, exists := seenIDs[resource.ID]; exists {
			return options, fmt.Errorf("duplicate registered Effect House script id %q", resource.ID)
		}
		seenIDs[resource.ID] = struct{}{}
		if err := validateRelativeScriptPath(resource.Path); err != nil {
			return options, err
		}
		if _, exists := seenPaths[resource.Path]; exists {
			return options, fmt.Errorf("duplicate registered Effect House script path %q", resource.Path)
		}
		seenPaths[resource.Path] = struct{}{}
	}
	return options, nil
}

func validateProjectRoot(value string) (string, error) {
	if !filepath.IsAbs(value) {
		return "", errors.New("Effect House local-project endpoint must be an absolute project directory")
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(value))
	if err != nil {
		return "", fmt.Errorf("resolve Effect House project root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", errors.New("Effect House local-project endpoint must be an existing directory")
	}
	return root, nil
}

func validateResources(root string, options Options) error {
	for _, resource := range options.RegisteredScripts {
		path := filepath.Join(root, filepath.FromSlash(resource.Path))
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve registered Effect House script %q: %w", resource.ID, err)
		}
		relative, err := filepath.Rel(root, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("registered Effect House script %q escapes project root", resource.ID)
		}
		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			return fmt.Errorf("registered Effect House script %q must be a file", resource.ID)
		}
	}
	return nil
}

func validateRelativeScriptPath(value string) error {
	if !fs.ValidPath(value) || !strings.HasSuffix(value, ".ts") {
		return errors.New("registered Effect House script path must be a relative .ts path")
	}
	base := strings.TrimSuffix(filepath.Base(value), ".ts")
	if !scriptName.MatchString(base) {
		return errors.New("registered Effect House script file name must use letters, digits, and underscores and cannot start with a digit")
	}
	return nil
}

func decodeScriptID(raw json.RawMessage) (scriptReadInput, error) {
	var input scriptReadInput
	if json.Unmarshal(raw, &input) != nil || strings.TrimSpace(input.ScriptID) == "" {
		return input, errors.New("script action requires scriptId")
	}
	return input, nil
}

func decodeReplaceInput(raw json.RawMessage) (scriptReplaceInput, error) {
	var input scriptReplaceInput
	if json.Unmarshal(raw, &input) != nil || strings.TrimSpace(input.ScriptID) == "" {
		return input, errors.New("script action requires scriptId")
	}
	if len(input.ExpectedSHA256) != 64 || !isHex(input.ExpectedSHA256) {
		return input, errors.New("script action requires expectedSha256")
	}
	return input, nil
}

func decodeCandidateInput(raw json.RawMessage) (scriptCandidateInput, error) {
	var input scriptCandidateInput
	if json.Unmarshal(raw, &input) != nil || strings.TrimSpace(input.ScriptID) == "" {
		return input, errors.New("script action requires scriptId")
	}
	return input, nil
}

func validateAPJSComponent(source string, limit int) error {
	if len(source) == 0 || len(source) > limit {
		return errors.New("APJS source is empty or exceeds source limit")
	}
	if !strings.Contains(source, "@component()") || !strings.Contains(source, "extends APJS.BasicScriptComponent") {
		return errors.New("APJS source must declare @component() class extending APJS.BasicScriptComponent")
	}
	return nil
}

func writeAtomic(path string, source []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".jangolova-effect-house-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(source); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func digest(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func hasWritable(resources []ScriptResource) bool {
	for _, resource := range resources {
		if resource.Writable {
			return true
		}
	}
	return false
}
func isHex(value string) bool { _, err := hex.DecodeString(value); return err == nil }
func missingCapabilities(required, offered []string) []string {
	var missing []string
	for _, value := range required {
		if !contains(offered, value) {
			missing = append(missing, value)
		}
	}
	return missing
}
func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
func compareCursor(left, right string) int {
	if left == right {
		return 0
	}
	if left > right {
		return 1
	}
	return -1
}
func objectSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false}`)
}
func scriptIDSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["scriptId"],"properties":{"scriptId":{"type":"string"}}}`)
}
func replaceSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["scriptId","expectedSha256","source"],"properties":{"scriptId":{"type":"string"},"expectedSha256":{"type":"string","pattern":"^[A-Fa-f0-9]{64}$"},"source":{"type":"string"}}}`)
}
func candidateSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["scriptId","source"],"properties":{"scriptId":{"type":"string"},"source":{"type":"string"}}}`)
}
