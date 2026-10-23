package deviceplugin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	extplugin "github.com/webong/ext/pkg/plugin-go/contract"
	"jangolova/devicekit"
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
	authorize := sessionPolicy(devicekit.Grant{Capabilities: []devicekit.Capability{devicekit.PointerMove}})
	ctx := context.Background()
	if err := authorize(ctx, request("invoke", devicekit.Action{Capability: devicekit.PointerMove})); err == nil {
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
	if err := authorize(ctx, request("invoke", devicekit.Action{Capability: devicekit.PointerMove})); err != nil {
		t.Fatalf("a granted action was refused: %v", err)
	}
	if err := authorize(ctx, request("close", nil)); err != nil {
		t.Fatal(err)
	}
}

func TestSessionProcessEnforcesTheGrant(t *testing.T) {
	authorize := sessionPolicy(devicekit.Grant{
		Capabilities: []devicekit.Capability{devicekit.DriveRead},
		ResourceIDs:  []string{"root"},
	})
	ctx := context.Background()
	if err := authorize(ctx, request("open", nil)); err != nil {
		t.Fatal(err)
	}
	if err := authorize(ctx, request("stream.open", devicekit.Action{Capability: devicekit.DriveRead, ResourceID: "root"})); err != nil {
		t.Fatalf("a granted stream was refused: %v", err)
	}
	for name, action := range map[string]devicekit.Action{
		"capability not granted": {Capability: devicekit.PointerClick},
		"resource not granted":   {Capability: devicekit.DriveRead, ResourceID: "other"},
	} {
		err := authorize(ctx, request("stream.open", action))
		if !errors.Is(err, devicekit.ErrNotGranted) {
			t.Fatalf("%s: error = %v", name, err)
		}
	}
	bad := extplugin.Request{Operation: "invoke", Payload: json.RawMessage(`"not an action"`)}
	if err := authorize(ctx, bad); !errors.Is(err, devicekit.ErrNotGranted) {
		t.Fatalf("unreadable action error = %v", err)
	}
}

func TestViewerPolicyFollowsTheLatestGrant(t *testing.T) {
	policy := ViewerPolicy()
	ask := func(operation, payload string) error {
		return policy(context.Background(), extplugin.Request{Operation: operation, Payload: json.RawMessage(payload)})
	}
	if err := ask("list", ""); err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := ask("invoke", `{"capability":"webrtc.poll"}`); err == nil {
		t.Fatal("invoke was admitted before any open")
	}
	if err := ask("open", `{"deviceId":"peer","grant":{"capabilities":["webrtc.poll"]}}`); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := ask("invoke", `{"capability":"webrtc.poll"}`); err != nil {
		t.Fatalf("granted invoke: %v", err)
	}
	if err := ask("invoke", `{"capability":"webrtc.signal"}`); !errors.Is(err, devicekit.ErrNotGranted) {
		t.Fatalf("ungranted invoke = %v", err)
	}
	if err := ask("close", ""); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := ask("invoke", `{"capability":"webrtc.poll"}`); err == nil {
		t.Fatal("invoke was admitted after close")
	}
	if err := ask("open", `{"deviceId":"peer","grant":{"capabilities":["webrtc.signal"]}}`); err != nil {
		t.Fatalf("second open: %v", err)
	}
	if err := ask("invoke", `{"capability":"webrtc.signal"}`); err != nil {
		t.Fatalf("second session invoke: %v", err)
	}
	if err := ask("reboot", ""); err == nil {
		t.Fatal("an unknown operation was admitted")
	}
}
