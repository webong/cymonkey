package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"cymonkey/src/internal/orchestrator"
	"cymonkey/src/internal/userscripts"
)

const userscriptAugmentationID = "cymonkey-managed-userscripts"

func replayUserscripts(ctx context.Context, instance orchestrator.EngineInstance, directory, target string) error {
	_, err := syncUserscripts(ctx, instance, directory, target, nil)
	return err
}

func syncUserscripts(ctx context.Context, instance orchestrator.EngineInstance, directory, target string, previous map[string]userscripts.Record) (map[string]userscripts.Record, error) {
	records, err := userscripts.List(directory, target)
	if err != nil {
		return nil, err
	}
	desired := make(map[string]userscripts.Record, len(records))
	for _, record := range records {
		if record.Enabled {
			desired[record.ID] = record
		}
	}
	caller, ok := instance.(interface {
		Call(context.Context, string, json.RawMessage) (json.RawMessage, error)
	})
	if !ok {
		if len(desired) != 0 {
			return nil, errors.New("selected engine cannot register userscripts")
		}
		return desired, nil
	}
	for id, record := range previous {
		if next, exists := desired[id]; exists && reflect.DeepEqual(next, record) {
			continue
		}
		request, err := json.Marshal(map[string]any{
			"name":  "script.unregister",
			"input": map[string]string{"augmentationId": userscriptAugmentationID, "id": id},
		})
		if err != nil {
			return nil, err
		}
		if _, err := caller.Call(ctx, "act", request); err != nil {
			return nil, fmt.Errorf("unregister userscript %q: %w", id, err)
		}
	}
	for _, record := range records {
		if !record.Enabled || reflect.DeepEqual(previous[record.ID], record) {
			continue
		}
		request, err := json.Marshal(map[string]any{
			"name": "script.register",
			"input": map[string]any{
				"augmentationId": userscriptAugmentationID,
				"script": map[string]string{
					"id": record.ID, "source": record.Source,
				},
				"matches": record.Matches, "excludeMatches": record.ExcludeMatches,
			},
		})
		if err != nil {
			return nil, err
		}
		if _, err := caller.Call(ctx, "act", request); err != nil {
			return nil, fmt.Errorf("register userscript %q: %w", record.ID, err)
		}
	}
	return desired, nil
}
