package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"strings"

	"cymonkey/src/internal/userscripts"
)

type userscriptOperatorRequest struct {
	TargetID       string   `json:"targetId,omitempty"`
	ID             string   `json:"id,omitempty"`
	Name           string   `json:"name,omitempty"`
	SourcePath     string   `json:"sourcePath,omitempty"`
	Revision       string   `json:"revision,omitempty"`
	Matches        []string `json:"matches,omitempty"`
	ExcludeMatches []string `json:"excludeMatches,omitempty"`
}

func (server *operatorServer) handleUserscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		operatorError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST is required")
		return
	}
	command := strings.TrimPrefix(r.URL.Path, "/v1/userscripts/")
	switch command {
	case "prepare", "install", "update", "list", "describe", "enable", "disable", "uninstall":
	default:
		http.NotFound(w, r)
		return
	}
	var request userscriptOperatorRequest
	if err := decodeOperatorJSON(w, r, &request); err != nil {
		operatorError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var output bytes.Buffer
	if err := executeUserscriptAction(userscriptOptions{
		Command: command, Store: server.store, Target: request.TargetID, ID: request.ID,
		Name: request.Name, Source: request.SourcePath, Revision: request.Revision,
		Matches: request.Matches, Excludes: request.ExcludeMatches,
	}, &output); err != nil {
		operatorError(w, http.StatusUnprocessableEntity, "userscript_action_failed", err.Error())
		return
	}
	if command == "install" || command == "update" || command == "enable" || command == "disable" || command == "uninstall" {
		target := request.TargetID
		if target == "" {
			var result struct {
				Target string `json:"target"`
			}
			if err := json.Unmarshal(output.Bytes(), &result); err == nil {
				target = result.Target
			}
		}
		if target != "" {
			if err := server.syncTargetUserscripts(r.Context(), target); err != nil {
				var saved map[string]any
				if json.Unmarshal(output.Bytes(), &saved) != nil {
					saved = map[string]any{"target": target}
				}
				saved["activationError"] = err.Error()
				operatorJSON(w, http.StatusAccepted, saved)
				return
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(output.Bytes())
}

func (server *operatorServer) syncTargetUserscripts(ctx context.Context, target string) error {
	server.mu.Lock()
	sessions := make(map[string]*operatorBrowserSession)
	for id, session := range server.active {
		if session.target == target {
			sessions[id] = session
		}
	}
	server.mu.Unlock()
	for id, session := range sessions {
		if err := server.syncUserscripts(ctx, id, session); err != nil {
			return fmt.Errorf("instance %s: %w", id, err)
		}
	}
	return nil
}

func (server *operatorServer) replayRecoveredUserscripts(ctx context.Context, instanceID string) error {
	server.mu.Lock()
	session := server.active[instanceID]
	server.mu.Unlock()
	if session == nil {
		return nil
	}
	session.mu.Lock()
	session.applied = nil
	session.mu.Unlock()
	if err := server.syncUserscripts(ctx, instanceID, session); err != nil {
		return fmt.Errorf("replay userscripts on recovered instance %s: %w", instanceID, err)
	}
	return nil
}

func (server *operatorServer) syncUserscripts(ctx context.Context, instanceID string, session *operatorBrowserSession) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	records, err := userscripts.List(server.store, session.target)
	if err != nil {
		return err
	}
	desired := make(map[string]userscripts.Record, len(records))
	current := maps.Clone(session.applied)
	if current == nil {
		current = make(map[string]userscripts.Record)
	}
	for _, record := range records {
		if record.Enabled {
			desired[record.ID] = record
		}
	}
	for id, previous := range session.applied {
		if next, found := desired[id]; found && reflect.DeepEqual(next, previous) {
			continue
		}
		if err := server.callUserscriptAction(ctx, instanceID, "script.unregister", map[string]any{
			"augmentationId": "cymonkey-managed-userscripts", "id": id,
		}); err != nil {
			session.applied = current
			return fmt.Errorf("unregister %s: %w", id, err)
		}
		delete(current, id)
	}
	for _, record := range records {
		if !record.Enabled || reflect.DeepEqual(session.applied[record.ID], record) {
			continue
		}
		if err := server.callUserscriptAction(ctx, instanceID, "script.register", map[string]any{
			"augmentationId": "cymonkey-managed-userscripts",
			"script":         map[string]string{"id": record.ID, "source": record.Source},
			"matches":        record.Matches, "excludeMatches": record.ExcludeMatches,
		}); err != nil {
			session.applied = current
			return fmt.Errorf("register %s: %w", record.ID, err)
		}
		current[record.ID] = record
	}
	session.applied = desired
	return nil
}

func (server *operatorServer) callUserscriptAction(ctx context.Context, instanceID, name string, input map[string]any) error {
	params, err := json.Marshal(map[string]any{"name": name, "input": input})
	if err != nil {
		return err
	}
	result := server.engineRequest(ctx, http.MethodPost, "/v1/instances/"+instanceID+"/call", map[string]any{"method": "act", "params": json.RawMessage(params)})
	if result.Code >= 200 && result.Code < 300 {
		return nil
	}
	var failure struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(result.Body.Bytes(), &failure) == nil && failure.Message != "" {
		return errors.New(failure.Message)
	}
	return fmt.Errorf("interaction provider returned HTTP %d", result.Code)
}
