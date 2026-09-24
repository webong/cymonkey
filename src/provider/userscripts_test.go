package provider

import (
	"context"
	"encoding/json"
	"testing"

	"cymonkey/src/internal/orchestrator"
	"cymonkey/src/internal/userscripts"
)

type userscriptCallerFixture struct{ calls []json.RawMessage }

func (*userscriptCallerFixture) Disconnect(context.Context) error { return nil }
func (*userscriptCallerFixture) Authorize(context.Context, orchestrator.AuthorizeRequest) (orchestrator.AuthorizeDecision, error) {
	return orchestrator.AuthorizeDecision{Authorized: true}, nil
}
func (fixture *userscriptCallerFixture) Call(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if method != "act" {
		panic("unexpected method")
	}
	fixture.calls = append(fixture.calls, append(json.RawMessage{}, params...))
	return json.RawMessage(`{"ok":true}`), nil
}

func TestReplayUserscriptsFiltersDisabledAndTarget(t *testing.T) {
	store := t.TempDir()
	for _, item := range []struct {
		target, id string
		enabled    bool
	}{
		{"selected", "enabled", true}, {"selected", "disabled", false}, {"other", "unrelated", true},
	} {
		source := "window.test = true;"
		record := userscripts.Record{Target: item.target, ID: item.id, Name: item.id,
			Matches:        []string{"https://example.com/*"},
			ExcludeMatches: []string{}, Enabled: item.enabled, Source: source}
		record.Revision = userscripts.Revision(record)
		if err := userscripts.Save(store, record, false); err != nil {
			t.Fatal(err)
		}
	}
	fixture := &userscriptCallerFixture{}
	if err := replayUserscripts(context.Background(), fixture, store, "selected"); err != nil {
		t.Fatal(err)
	}
	if len(fixture.calls) != 1 {
		t.Fatalf("wanted one registered script, got %d", len(fixture.calls))
	}
	var call struct {
		Name  string `json:"name"`
		Input struct {
			Script struct {
				ID     string `json:"id"`
				Source string `json:"source"`
			} `json:"script"`
		} `json:"input"`
	}
	if err := json.Unmarshal(fixture.calls[0], &call); err != nil {
		t.Fatal(err)
	}
	if call.Name != "script.register" || call.Input.Script.ID != "enabled" || call.Input.Script.Source != "window.test = true;" {
		t.Fatalf("unexpected registration: %+v", call)
	}
}

func TestSyncUserscriptsAppliesDisableAndUpdate(t *testing.T) {
	store := t.TempDir()
	record := userscripts.Record{Target: "selected", ID: "example", Name: "Example",
		Source: "window.version = 1;", Matches: []string{"https://example.com/*"},
		ExcludeMatches: []string{}, Enabled: true}
	record.Revision = userscripts.Revision(record)
	if err := userscripts.Save(store, record, false); err != nil {
		t.Fatal(err)
	}
	fixture := &userscriptCallerFixture{}
	applied, err := syncUserscripts(context.Background(), fixture, store, "selected", nil)
	if err != nil || len(fixture.calls) != 1 {
		t.Fatalf("initial sync: %v, %d calls", err, len(fixture.calls))
	}
	record.Source = "window.version = 2;"
	record.Revision = userscripts.Revision(record)
	if err := userscripts.Save(store, record, true); err != nil {
		t.Fatal(err)
	}
	applied, err = syncUserscripts(context.Background(), fixture, store, "selected", applied)
	if err != nil || len(fixture.calls) != 3 {
		t.Fatalf("update sync: %v, %d calls", err, len(fixture.calls))
	}
	var removed struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(fixture.calls[1], &removed); err != nil || removed.Name != "script.unregister" {
		t.Fatalf("old script was not removed: %s, %v", fixture.calls[1], err)
	}
	if _, err := userscripts.SetEnabled(store, "selected", "example", false); err != nil {
		t.Fatal(err)
	}
	applied, err = syncUserscripts(context.Background(), fixture, store, "selected", applied)
	if err != nil || len(applied) != 0 || len(fixture.calls) != 4 {
		t.Fatalf("disable sync: %v, %d applied, %d calls", err, len(applied), len(fixture.calls))
	}
}
