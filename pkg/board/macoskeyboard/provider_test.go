package macoskeyboard

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"board"
)

type fakeNative struct {
	permission bool
	identityID processIdentity
	presses    int
	typed      string
}

func (f *fakeNative) trusted() bool                         { return f.permission }
func (f *fakeNative) identity(int) (processIdentity, error) { return f.identityID, nil }
func (f *fakeNative) press(_ int, _ uint16, _ uint64) error { f.presses++; return nil }
func (f *fakeNative) typeRune(_ int, chars []uint16) error {
	f.typed += string(rune(chars[0]))
	return nil
}

func TestKeyboardRequiresExplicitTargetAndPermission(t *testing.T) {
	fake := &fakeNative{permission: true, identityID: processIdentity{seconds: 10}}
	if _, err := newWithNative(Options{ProviderID: "test"}, fake); err == nil {
		t.Fatal("keyboard without a target PID was accepted")
	}
	if strconv.IntSize == 64 {
		var hugePID int64 = 1 << 40
		if _, err := newWithNative(Options{ProviderID: "test", PID: int(hugePID)}, fake); err == nil {
			t.Fatal("keyboard PID outside pid_t range was accepted")
		}
	}
	fake.permission = false
	if _, err := newWithNative(Options{ProviderID: "test", PID: 42}, fake); !errors.Is(err, ErrPermission) {
		t.Fatalf("missing Accessibility permission = %v", err)
	}
}

func TestKeyboardGrantAndTargetIdentity(t *testing.T) {
	ctx := context.Background()
	fake := &fakeNative{permission: true, identityID: processIdentity{seconds: 10}}
	provider, err := newWithNative(Options{ProviderID: "mac-keyboard", PID: 42, MaxTextRunes: 8}, fake)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := board.NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := registry.List(ctx)
	if err != nil || len(devices) != 1 || len(devices[0].Capabilities) != 2 {
		t.Fatalf("keyboard discovery = %#v, %v", devices, err)
	}
	connection, err := registry.Open(ctx, board.OpenRequest{
		ProviderID: provider.ID(), DeviceID: "keyboard",
		Grant: board.Grant{Capabilities: []board.Capability{board.KeyboardPress}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(ctx)
	if _, err := connection.Invoke(ctx, board.Action{
		Capability: board.KeyboardType, Input: json.RawMessage(`{"text":"secret"}`),
	}); err == nil || fake.typed != "" {
		t.Fatalf("ungranted typing reached target: %q, %v", fake.typed, err)
	}
	if _, err := connection.Invoke(ctx, board.Action{
		Capability: board.KeyboardPress, Input: json.RawMessage(`{"key":"Command+O"}`),
	}); err != nil || fake.presses != 1 {
		t.Fatalf("press = %d, %v", fake.presses, err)
	}
	fake.identityID.seconds++
	if _, err := connection.Invoke(ctx, board.Action{
		Capability: board.KeyboardPress, Input: json.RawMessage(`{"key":"Enter"}`),
	}); !errors.Is(err, ErrTarget) || fake.presses != 1 {
		t.Fatalf("reused PID was accepted: %d, %v", fake.presses, err)
	}
}

func TestKeyboardTypeLimitsAndInputValidation(t *testing.T) {
	ctx := context.Background()
	fake := &fakeNative{permission: true, identityID: processIdentity{seconds: 10}}
	provider, err := newWithNative(Options{ProviderID: "mac-keyboard", PID: 42, MaxTextRunes: 4}, fake)
	if err != nil {
		t.Fatal(err)
	}
	session, err := provider.Open(ctx, "keyboard", board.Grant{Capabilities: []board.Capability{board.KeyboardType}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)
	for _, input := range []string{
		`{"text":""}`, `{"text":"abcde"}`, `{"text":"a\nb"}`,
		`{"text":"ok","extra":true}`, `{"text":"ok"} {}`,
	} {
		if _, err := session.Invoke(ctx, board.Action{Capability: board.KeyboardType, Input: json.RawMessage(input)}); err == nil {
			t.Fatalf("invalid keyboard input %q was accepted", input)
		}
	}
	if _, err := session.Invoke(ctx, board.Action{
		Capability: board.KeyboardType, Input: json.RawMessage(`{"text":"okay"}`),
	}); err != nil || fake.typed != "okay" {
		t.Fatalf("typed = %q, %v", fake.typed, err)
	}
	fake.permission = false
	if _, err := session.Invoke(ctx, board.Action{
		Capability: board.KeyboardType, Input: json.RawMessage(`{"text":"x"}`),
	}); !errors.Is(err, ErrPermission) {
		t.Fatalf("revoked permission = %v", err)
	}
}

func TestPressKeysAreBounded(t *testing.T) {
	code, flags, err := parseKey("Command+Shift+O")
	if err != nil || code != 31 || flags != modifierCommand|modifierShift {
		t.Fatalf("Command+Shift+O = %d, %d, %v", code, flags, err)
	}
	for _, key := range []string{"", "Command", "Command+Command+O", "Alt+O", "Command+F1", "Command+O+P"} {
		if _, _, err := parseKey(key); err == nil {
			t.Fatalf("unsupported key %q was accepted", key)
		}
	}
	if !strings.Contains(ErrUnsupported.Error(), "unavailable") {
		t.Fatal("unsupported platform error should be actionable")
	}
}
