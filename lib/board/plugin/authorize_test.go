package boardplugin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"board"
	extplugin "github.com/webong/ext/pkg/plugin-go/contract"
)

func request(operation string, payload any) extplugin.Request {
	raw, _ := json.Marshal(payload)
	return extplugin.Request{Operation: operation, Payload: raw}
}

func TestListProcessAdmitsOnlyList(t *testing.T) {
	authorize := listPolicy()
	if err := authorize(context.Background(), request("list", nil)); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"open", "invoke", "stream.open", "close"} {
		if err := authorize(context.Background(), request(operation, nil)); err == nil {
			t.Fatalf("a list process admitted %s", operation)
		}
	}
}

func TestSessionProcessOpensOnceBeforeAnythingElse(t *testing.T) {
	authorize := sessionPolicy(board.Grant{Capabilities: []board.Capability{board.PointerMove}})
	ctx := context.Background()
	if err := authorize(ctx, request("invoke", board.Action{Capability: board.PointerMove})); err == nil {
		t.Fatal("invoke was admitted before open")
	}
	if err := authorize(ctx, request("list", nil)); err == nil {
		t.Fatal("a session process admitted list")
	}
	if err := authorize(ctx, request("open", nil)); err != nil {
		t.Fatal(err)
	}
	if err := authorize(ctx, request("open", nil)); err == nil {
		t.Fatal("open was admitted twice")
	}
	if err := authorize(ctx, request("invoke", board.Action{Capability: board.PointerMove})); err != nil {
		t.Fatalf("a granted action was refused: %v", err)
	}
	if err := authorize(ctx, request("close", nil)); err != nil {
		t.Fatal(err)
	}
}

func TestSessionProcessEnforcesTheGrant(t *testing.T) {
	authorize := sessionPolicy(board.Grant{
		Capabilities: []board.Capability{board.DriveRead},
		ResourceIDs:  []string{"root"},
	})
	ctx := context.Background()
	if err := authorize(ctx, request("open", nil)); err != nil {
		t.Fatal(err)
	}
	if err := authorize(ctx, request("stream.open", board.Action{Capability: board.DriveRead, ResourceID: "root"})); err != nil {
		t.Fatalf("a granted stream was refused: %v", err)
	}
	for name, action := range map[string]board.Action{
		"capability not granted": {Capability: board.PointerClick},
		"resource not granted":   {Capability: board.DriveRead, ResourceID: "other"},
	} {
		err := authorize(ctx, request("stream.open", action))
		if !errors.Is(err, board.ErrNotGranted) {
			t.Fatalf("%s: error = %v", name, err)
		}
	}
	bad := extplugin.Request{Operation: "invoke", Payload: json.RawMessage(`"not an action"`)}
	if err := authorize(ctx, bad); !errors.Is(err, board.ErrNotGranted) {
		t.Fatalf("unreadable action error = %v", err)
	}
}
