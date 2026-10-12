// Package console provides a small, provider-neutral tool catalog and
// invocation API for applications that compose model and interface SDKs.
//
// Console does not choose tools or run an agent loop. ModelTools performs one
// explicitly requested model turn. The caller owns transcripts, approvals,
// tool invocation, and whether to request another turn.
package console

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/webong/ext/pkg/plugin/jsonschema"
)

var (
	ErrInvalidTool   = errors.New("invalid Console tool")
	ErrDuplicateTool = errors.New("Console tool already registered")
	ErrToolNotFound  = errors.New("Console tool not found")
	ErrInvalidCall   = errors.New("invalid Console tool call")
	ErrInvalidResult = errors.New("invalid Console tool result")
)

// Definition describes a callable tool in a provider-neutral form. InputSchema
// is a JSON Schema object whose root describes the tool's argument object.
type Definition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Call is one explicit request to invoke a registered tool. Calls are data;
// receiving a Call does not imply authorization to execute it.
type Call struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Result contains the JSON value returned by a tool. The caller decides how to
// present it to a person or feed it back to a model.
type Result struct {
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name"`
	Output  json.RawMessage `json:"output"`
	IsError bool            `json:"isError,omitempty"`
}

// Handler executes one tool call after schema validation. Handlers also enforce
// semantic constraints, resource authorization and their own output bounds.
type Handler func(context.Context, json.RawMessage) (json.RawMessage, error)

// Tool binds a public definition to its implementation.
type Tool struct {
	definition    Definition
	handler       Handler
	resultHandler func(context.Context, json.RawMessage) (Result, error)
	schema        *jsonschema.Schema
}

// NewResultTool preserves a tool's explicit failure flag as well as JSON output.
// Call IDs and tool names are supplied by the registry, never by the handler.
func NewResultTool(definition Definition, handler func(context.Context, json.RawMessage) (Result, error)) (Tool, error) {
	if handler == nil {
		return Tool{}, fmt.Errorf("%w: handler is required", ErrInvalidTool)
	}
	tool, err := NewTool(definition, func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, ErrInvalidResult })
	if err != nil {
		return tool, err
	}
	tool.resultHandler = handler
	return tool, nil
}

// NewTool binds a JSON Schema definition to a handler.
func NewTool(definition Definition, handler Handler) (Tool, error) {
	if err := validateDefinition(definition); err != nil {
		return Tool{}, err
	}
	if handler == nil {
		return Tool{}, fmt.Errorf("%w: handler is required", ErrInvalidTool)
	}
	definition.InputSchema = append(json.RawMessage(nil), definition.InputSchema...)
	schema, err := jsonschema.Compile(definition.InputSchema)
	if err != nil {
		return Tool{}, fmt.Errorf("%w: %w", ErrInvalidTool, err)
	}
	return Tool{definition: definition, handler: handler, schema: schema}, nil
}

// NewJSONTool adapts typed JSON input and output to the provider-neutral
// Handler contract. Unknown input fields and trailing JSON values are rejected.
func NewJSONTool[Input any, Output any](definition Definition, handler func(context.Context, Input) (Output, error)) (Tool, error) {
	if handler == nil {
		return Tool{}, fmt.Errorf("%w: handler is required", ErrInvalidTool)
	}
	return NewTool(definition, func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		var input Input
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return nil, fmt.Errorf("decode tool arguments: %w", err)
		}
		if err := requireEOF(decoder); err != nil {
			return nil, fmt.Errorf("decode tool arguments: %w", err)
		}
		output, err := handler(ctx, input)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(output)
		if err != nil {
			return nil, fmt.Errorf("encode tool output: %w", err)
		}
		return encoded, nil
	})
}

// Definition returns a copy of the tool's public metadata.
func (t Tool) Definition() Definition {
	definition := t.definition
	definition.InputSchema = append(json.RawMessage(nil), definition.InputSchema...)
	return definition
}

// Registry stores tools and explicitly dispatches individual calls. Its zero
// value is ready to use. A Registry does not run model/tool loops.
type Registry struct {
	mu     sync.RWMutex
	byName map[string]Tool
}

// Register adds a tool. Names are unique within a registry.
func (r *Registry) Register(tool Tool) error {
	if err := validateDefinition(tool.definition); err != nil {
		return err
	}
	if tool.handler == nil {
		return fmt.Errorf("%w: handler is required", ErrInvalidTool)
	}
	name := tool.definition.Name
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byName == nil {
		r.byName = make(map[string]Tool)
	}
	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateTool, name)
	}
	tool.definition.InputSchema = append(json.RawMessage(nil), tool.definition.InputSchema...)
	r.byName[name] = tool
	return nil
}

// Definitions returns a stable, name-sorted copy of the available tools.
func (r *Registry) Definitions() []Definition {
	r.mu.RLock()
	definitions := make([]Definition, 0, len(r.byName))
	for _, tool := range r.byName {
		definitions = append(definitions, tool.Definition())
	}
	r.mu.RUnlock()
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

// Invoke executes exactly one named tool. Callers should apply their own
// approval and policy checks before invoking tools that affect external state.
func (r *Registry) Invoke(ctx context.Context, call Call) (Result, error) {
	if ctx == nil || !validName(call.Name) || !validJSONObject(call.Arguments) {
		return Result{}, ErrInvalidCall
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	r.mu.RLock()
	tool, ok := r.byName[call.Name]
	r.mu.RUnlock()
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", ErrToolNotFound, call.Name)
	}
	if err := tool.schema.Validate(call.Arguments); err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalidCall, err)
	}
	arguments := append(json.RawMessage(nil), call.Arguments...)
	var output json.RawMessage
	var isError bool
	var err error
	if tool.resultHandler != nil {
		var result Result
		result, err = tool.resultHandler(ctx, arguments)
		output, isError = result.Output, result.IsError
	} else {
		output, err = tool.handler(ctx, arguments)
	}
	if err != nil {
		return Result{}, fmt.Errorf("invoke Console tool %q: %w", call.Name, err)
	}
	if len(output) > 32<<20 || !json.Valid(output) {
		return Result{}, fmt.Errorf("%w: tool %q did not return valid JSON", ErrInvalidResult, call.Name)
	}
	return Result{ID: call.ID, Name: call.Name, Output: append(json.RawMessage(nil), output...), IsError: isError}, nil
}

func validateDefinition(definition Definition) error {
	if !validName(definition.Name) {
		return fmt.Errorf("%w: name must be 1-128 printable characters without whitespace", ErrInvalidTool)
	}
	if len(definition.Description) > 16<<10 || !json.Valid(definition.InputSchema) {
		return fmt.Errorf("%w: description or input schema is invalid", ErrInvalidTool)
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil || schema == nil {
		return fmt.Errorf("%w: input schema must be a JSON object", ErrInvalidTool)
	}
	return nil
}

func validName(name string) bool {
	if len(name) == 0 || len(name) > 128 || strings.TrimSpace(name) != name {
		return false
	}
	for _, char := range name {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func validJSONObject(raw json.RawMessage) bool {
	if !json.Valid(raw) {
		return false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return false
	}
	return true
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
