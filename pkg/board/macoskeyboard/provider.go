// Package macoskeyboard binds Board keyboard actions to a caller-selected
// macOS process. It never chooses or launches a target process.
package macoskeyboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"board"
)

var (
	ErrUnsupported = errors.New("macOS keyboard provider is unavailable on this build")
	ErrPermission  = errors.New("macOS Accessibility permission is required for keyboard input")
	ErrTarget      = errors.New("macOS keyboard target is unavailable or has changed")
)

const DefaultMaxTextRunes = 1024

type Options struct {
	ProviderID   string
	DeviceID     string
	PID          int
	Name         string
	MaxTextRunes int
}

type processIdentity struct {
	seconds      uint64
	microseconds uint64
}

// nativeKeyboard is kept small so permission and target checks can be tested
// without sending input to another process.
type nativeKeyboard interface {
	trusted() bool
	identity(pid int) (processIdentity, error)
	press(pid int, code uint16, modifiers uint64) error
	typeRune(pid int, chars []uint16) error
}

type Provider struct {
	options  Options
	native   nativeKeyboard
	identity processIdentity
}

var _ board.Provider = (*Provider)(nil)

// New prepares a provider for exactly one caller-selected process. It checks
// Accessibility permission and records the process start identity so a reused
// PID cannot silently receive a later action.
func New(options Options) (*Provider, error) {
	native, err := newNativeKeyboard()
	if err != nil {
		return nil, err
	}
	return newWithNative(options, native)
}

func newWithNative(options Options, native nativeKeyboard) (*Provider, error) {
	if native == nil {
		return nil, ErrUnsupported
	}
	options.ProviderID = strings.TrimSpace(options.ProviderID)
	options.DeviceID = strings.TrimSpace(options.DeviceID)
	if options.ProviderID == "" || options.PID <= 0 || options.PID > math.MaxInt32 {
		return nil, errors.New("macOS keyboard needs a provider ID and a valid positive target PID")
	}
	if options.DeviceID == "" {
		options.DeviceID = "keyboard"
	}
	if options.MaxTextRunes < 0 {
		return nil, errors.New("macOS keyboard text limit cannot be negative")
	}
	if options.MaxTextRunes == 0 {
		options.MaxTextRunes = DefaultMaxTextRunes
	}
	if !native.trusted() {
		return nil, ErrPermission
	}
	identity, err := native.identity(options.PID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTarget, err)
	}
	return &Provider{options: options, native: native, identity: identity}, nil
}

func (p *Provider) ID() string { return p.options.ProviderID }

func (p *Provider) checkTarget() error {
	if !p.native.trusted() {
		return ErrPermission
	}
	identity, err := p.native.identity(p.options.PID)
	if err != nil || identity != p.identity {
		return ErrTarget
	}
	return nil
}

func (p *Provider) List(ctx context.Context) ([]board.Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := p.checkTarget(); err != nil {
		return nil, err
	}
	return []board.Device{{
		ID: p.options.DeviceID, Kind: "keyboard", Name: p.options.Name,
		Capabilities: []board.CapabilityDescriptor{
			{Name: board.KeyboardPress, InputSchema: json.RawMessage(pressSchema), OutputSchema: json.RawMessage(submittedSchema)},
			{Name: board.KeyboardType, InputSchema: json.RawMessage(fmt.Sprintf(typeSchema, p.options.MaxTextRunes)), OutputSchema: json.RawMessage(submittedSchema)},
		},
	}}, nil
}

func (p *Provider) Open(ctx context.Context, deviceID string, grant board.Grant) (board.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deviceID != p.options.DeviceID {
		return nil, fmt.Errorf("macOS keyboard device %q is not registered", deviceID)
	}
	if err := p.checkTarget(); err != nil {
		return nil, err
	}
	if len(grant.Capabilities) == 0 || len(grant.ResourceIDs) != 0 {
		return nil, errors.New("macOS keyboard grant needs capabilities and no drive resources")
	}
	allowed := make(map[board.Capability]struct{}, len(grant.Capabilities))
	for _, capability := range grant.Capabilities {
		if capability != board.KeyboardPress && capability != board.KeyboardType {
			return nil, fmt.Errorf("macOS keyboard does not provide %q", capability)
		}
		allowed[capability] = struct{}{}
	}
	return &session{provider: p, allowed: allowed}, nil
}

type session struct {
	mu       sync.Mutex
	provider *Provider
	allowed  map[board.Capability]struct{}
	closed   bool
}

var _ board.Session = (*session)(nil)

func (s *session) Invoke(ctx context.Context, action board.Action) (board.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return board.Result{}, board.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return board.Result{}, err
	}
	if _, allowed := s.allowed[action.Capability]; !allowed {
		return board.Result{}, fmt.Errorf("macOS keyboard capability %q is not granted", action.Capability)
	}
	if action.ResourceID != "" {
		return board.Result{}, errors.New("keyboard actions do not accept a resource ID")
	}
	if err := s.provider.checkTarget(); err != nil {
		return board.Result{}, err
	}
	switch action.Capability {
	case board.KeyboardPress:
		var input struct {
			Key string `json:"key"`
		}
		if err := strictInput(action.Input, &input); err != nil {
			return board.Result{}, err
		}
		code, modifiers, err := parseKey(input.Key)
		if err != nil {
			return board.Result{}, err
		}
		if err := s.provider.native.press(s.provider.options.PID, code, modifiers); err != nil {
			return board.Result{}, err
		}
	case board.KeyboardType:
		var input struct {
			Text string `json:"text"`
		}
		if err := strictInput(action.Input, &input); err != nil {
			return board.Result{}, err
		}
		if !utf8.ValidString(input.Text) || input.Text == "" || utf8.RuneCountInString(input.Text) > s.provider.options.MaxTextRunes {
			return board.Result{}, errors.New("keyboard text is empty, invalid, or exceeds its limit")
		}
		for _, char := range input.Text {
			if unicode.IsControl(char) {
				return board.Result{}, errors.New("keyboard text contains a control character; use keyboard.press")
			}
		}
		for _, char := range input.Text {
			if err := ctx.Err(); err != nil {
				return board.Result{}, err
			}
			if err := s.provider.checkTarget(); err != nil {
				return board.Result{}, err
			}
			if err := s.provider.native.typeRune(s.provider.options.PID, utf16.Encode([]rune{char})); err != nil {
				return board.Result{}, err
			}
		}
	default:
		return board.Result{}, fmt.Errorf("macOS keyboard cannot invoke %q", action.Capability)
	}
	return board.Result{Output: json.RawMessage(`{"submitted":true}`)}, nil
}

func (s *session) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func strictInput(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return errors.New("keyboard action needs an input object")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode keyboard input: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("keyboard input has extra JSON values")
		}
		return fmt.Errorf("decode keyboard input: %w", err)
	}
	return nil
}

const (
	modifierCommand uint64 = 1 << iota
	modifierControl
	modifierOption
	modifierShift
)

var namedKeys = map[string]uint16{
	"enter": 36, "tab": 48, "escape": 53, "backspace": 51,
	"delete": 117, "space": 49, "arrowleft": 123, "arrowright": 124,
	"arrowdown": 125, "arrowup": 126,
}

// These are physical ANSI key positions. Typing text uses Unicode events and
// does not depend on the user's keyboard layout.
var letterCodes = map[byte]uint16{
	'a': 0, 'b': 11, 'c': 8, 'd': 2, 'e': 14, 'f': 3, 'g': 5,
	'h': 4, 'i': 34, 'j': 38, 'k': 40, 'l': 37, 'm': 46, 'n': 45,
	'o': 31, 'p': 35, 'q': 12, 'r': 15, 's': 1, 't': 17, 'u': 32,
	'v': 9, 'w': 13, 'x': 7, 'y': 16, 'z': 6,
}

func parseKey(value string) (uint16, uint64, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), "+")
	if len(parts) == 0 {
		return 0, 0, errors.New("keyboard key is empty")
	}
	var flags uint64
	for _, part := range parts[:len(parts)-1] {
		var flag uint64
		switch strings.TrimSpace(part) {
		case "command":
			flag = modifierCommand
		case "control":
			flag = modifierControl
		case "option":
			flag = modifierOption
		case "shift":
			flag = modifierShift
		default:
			return 0, 0, fmt.Errorf("unknown keyboard modifier %q", part)
		}
		if flags&flag != 0 {
			return 0, 0, fmt.Errorf("duplicate keyboard modifier %q", part)
		}
		flags |= flag
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	if code, ok := namedKeys[key]; ok {
		return code, flags, nil
	}
	if len(key) == 1 {
		if code, ok := letterCodes[key[0]]; ok {
			return code, flags, nil
		}
	}
	return 0, 0, fmt.Errorf("unsupported keyboard key %q", key)
}

const pressSchema = `{"type":"object","properties":{"key":{"type":"string","description":"A named key or Command, Control, Option, or Shift plus one named key or ANSI letter."}},"required":["key"],"additionalProperties":false}`
const typeSchema = `{"type":"object","properties":{"text":{"type":"string","minLength":1,"maxLength":%d,"description":"Unicode text without control characters."}},"required":["text"],"additionalProperties":false}`
const submittedSchema = `{"type":"object","properties":{"submitted":{"type":"boolean"}},"required":["submitted"],"additionalProperties":false}`
