package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schedule"
	"organization-autorunner-core/internal/schema"
)

func handleCreateThread(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID string         `json:"actor_id"`
		Thread  map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	if req.Thread == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "thread is required")
		return
	}
	if _, has := req.Thread["open_commitments"]; has {
		writeError(w, http.StatusBadRequest, "invalid_request", "thread.open_commitments is core-maintained and cannot be set")
		return
	}

	actorID, ok := requireRegisteredActorID(w, r, opts.actorRegistry, req.ActorID)
	if !ok {
		return
	}

	if err := validateThreadCreate(opts.contract, req.Thread); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := opts.primitiveStore.CreateThread(r.Context(), actorID, req.Thread)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create thread")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"thread": result.Snapshot})
}

func handleGetThread(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	thread, err := opts.primitiveStore.GetThread(r.Context(), threadID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load thread")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"thread": thread})
}

func handlePatchThread(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID     string         `json:"actor_id"`
		Patch       map[string]any `json:"patch"`
		IfUpdatedAt *string        `json:"if_updated_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	if req.Patch == nil || len(req.Patch) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch is required")
		return
	}
	if req.IfUpdatedAt != nil {
		ifUpdatedAt := strings.TrimSpace(*req.IfUpdatedAt)
		if _, err := time.Parse(time.RFC3339, ifUpdatedAt); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "if_updated_at must be an RFC3339 datetime string")
			return
		}
		req.IfUpdatedAt = &ifUpdatedAt
	}
	if _, has := req.Patch["open_commitments"]; has {
		writeError(w, http.StatusBadRequest, "invalid_request", "thread.open_commitments is core-maintained and cannot be patched")
		return
	}

	actorID, ok := requireRegisteredActorID(w, r, opts.actorRegistry, req.ActorID)
	if !ok {
		return
	}

	if err := validateThreadPatch(opts.contract, req.Patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := opts.primitiveStore.PatchThread(r.Context(), actorID, threadID, req.Patch, req.IfUpdatedAt)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
			return
		}
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "thread has been updated; refresh and retry")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to patch thread")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"thread": result.Snapshot})
}

func handleListThreads(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	query := r.URL.Query()
	tagsFilter := normalizedQueryValues(query["tag"])
	cadenceFilter := normalizedQueryValues(query["cadence"])
	var staleFilter *bool
	staleRaw := strings.TrimSpace(query.Get("stale"))
	if staleRaw != "" {
		parsed, err := strconv.ParseBool(staleRaw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "stale must be true or false")
			return
		}
		staleFilter = &parsed
	}

	threads, err := opts.primitiveStore.ListThreads(r.Context(), primitives.ThreadListFilter{
		Status:   strings.TrimSpace(query.Get("status")),
		Priority: strings.TrimSpace(query.Get("priority")),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list threads")
		return
	}

	if len(tagsFilter) > 0 || len(cadenceFilter) > 0 {
		filtered := make([]map[string]any, 0, len(threads))
		for _, thread := range threads {
			if !threadMatchesTagsAndCadence(thread, tagsFilter, cadenceFilter) {
				continue
			}
			filtered = append(filtered, thread)
		}
		threads = filtered
	}

	if staleFilter != nil {
		events, err := opts.primitiveStore.ListEvents(r.Context(), primitives.EventListFilter{
			Types: []string{"receipt_added", "decision_made"},
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to evaluate thread staleness")
			return
		}

		now := time.Now().UTC()
		staleByThread := stalenessByThread(threads, events, now)

		filtered := make([]map[string]any, 0, len(threads))
		for _, thread := range threads {
			threadID, _ := thread["id"].(string)
			if staleByThread[threadID] == *staleFilter {
				filtered = append(filtered, thread)
			}
		}
		threads = filtered
	}

	writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
}

func normalizedQueryValues(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}

	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func threadMatchesTagsAndCadence(thread map[string]any, tags []string, cadences []string) bool {
	if len(tags) > 0 {
		threadTags, err := extractStringSlice(thread["tags"])
		if err != nil {
			return false
		}
		for _, wantedTag := range tags {
			if !containsStringValue(threadTags, wantedTag) {
				return false
			}
		}
	}

	if len(cadences) > 0 {
		threadCadence, _ := thread["cadence"].(string)
		matchedCadence := false
		for _, cadenceFilter := range cadences {
			if schedule.CadenceMatchesFilter(threadCadence, cadenceFilter) {
				matchedCadence = true
				break
			}
		}
		if !matchedCadence {
			return false
		}
	}

	return true
}

func containsStringValue(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func handleThreadTimeline(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	if _, err := opts.primitiveStore.GetThread(r.Context(), threadID); err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load thread")
		return
	}

	events, err := opts.primitiveStore.ListEventsByThread(r.Context(), threadID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load thread timeline")
		return
	}

	snapshotIDs, artifactIDs := collectTimelineReferencedObjectIDs(events)

	snapshots := make(map[string]map[string]any, len(snapshotIDs))
	for _, snapshotID := range snapshotIDs {
		snapshot, err := opts.primitiveStore.GetSnapshot(r.Context(), snapshotID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load referenced snapshots")
			return
		}
		snapshots[snapshotID] = snapshot
	}

	artifacts := make(map[string]map[string]any, len(artifactIDs))
	for _, artifactID := range artifactIDs {
		artifact, err := opts.primitiveStore.GetArtifact(r.Context(), artifactID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load referenced artifacts")
			return
		}
		artifacts[artifactID] = artifact
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events":    events,
		"snapshots": snapshots,
		"artifacts": artifacts,
	})
}

func collectTimelineReferencedObjectIDs(events []map[string]any) ([]string, []string) {
	snapshotSet := make(map[string]struct{})
	artifactSet := make(map[string]struct{})

	for _, event := range events {
		refs, err := extractStringSlice(event["refs"])
		if err != nil {
			continue
		}
		for _, ref := range refs {
			prefix, id, err := schema.SplitTypedRef(ref)
			if err != nil {
				continue
			}
			switch prefix {
			case "snapshot":
				snapshotSet[id] = struct{}{}
			case "artifact":
				artifactSet[id] = struct{}{}
			}
		}
	}

	snapshotIDs := mapKeysSorted(snapshotSet)
	artifactIDs := mapKeysSorted(artifactSet)
	return snapshotIDs, artifactIDs
}

func mapKeysSorted(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func validateThreadCreate(contract *schema.Contract, thread map[string]any) error {
	threadSchema, ok := contract.Snapshots["thread"]
	if !ok {
		return fmt.Errorf("thread schema is not loaded")
	}

	required := make([]string, 0)
	for name, field := range threadSchema.Fields {
		if field.Required && name != "open_commitments" {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	for _, name := range required {
		value, exists := thread[name]
		if !exists {
			return fmt.Errorf("thread.%s is required", name)
		}
		if err := validateThreadField(contract, name, value, true); err != nil {
			return err
		}
	}

	for name, value := range thread {
		if err := validateThreadField(contract, name, value, true); err != nil {
			return err
		}
	}

	return nil
}

func validateThreadPatch(contract *schema.Contract, patch map[string]any) error {
	for name, value := range patch {
		if err := validateThreadField(contract, name, value, false); err != nil {
			return err
		}
	}
	return nil
}

func validateThreadField(contract *schema.Contract, fieldName string, value any, createMode bool) error {
	threadSchema, ok := contract.Snapshots["thread"]
	if !ok {
		return fmt.Errorf("thread schema is not loaded")
	}
	field, known := threadSchema.Fields[fieldName]
	if !known {
		// Unknown fields are allowed and preserved by patch/merge semantics.
		return nil
	}

	switch field.Type {
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("thread.%s must be a string", fieldName)
		}
		if createMode && field.Required && strings.TrimSpace(text) == "" {
			return fmt.Errorf("thread.%s must be non-empty", fieldName)
		}
		if fieldName == "cadence" {
			if err := schedule.ValidateCadence(text); err != nil {
				return fmt.Errorf("thread.%s: %w", fieldName, err)
			}
		}
		if strings.HasPrefix(field.Ref, "enums.") {
			enumName := strings.TrimPrefix(field.Ref, "enums.")
			if err := schema.ValidateEnum(contract, enumName, text); err != nil {
				return fmt.Errorf("thread.%s: %w", fieldName, err)
			}
		}
	case "datetime":
		if value == nil {
			return nil
		}
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("thread.%s must be an RFC3339 datetime string", fieldName)
		}
		if _, err := time.Parse(time.RFC3339, text); err != nil {
			return fmt.Errorf("thread.%s must be an RFC3339 datetime string", fieldName)
		}
	case "list<string>":
		if _, err := extractStringSlice(value); err != nil {
			return fmt.Errorf("thread.%s must be a list of strings", fieldName)
		}
	case "list<typed_ref>":
		refs, err := extractStringSlice(value)
		if err != nil {
			return fmt.Errorf("thread.%s must be a list of strings", fieldName)
		}
		if err := schema.ValidateTypedRefs(contract, refs); err != nil {
			return fmt.Errorf("thread.%s: %w", fieldName, err)
		}
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("thread.%s must be an object", fieldName)
		}
		if field.Ref == "provenance" {
			if err := schema.ValidateProvenance(contract, obj); err != nil {
				return fmt.Errorf("thread.%s: %w", fieldName, err)
			}
		}
	}

	return nil
}
