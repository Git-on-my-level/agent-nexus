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
		ActorID string         `json:"actor_id"`
		Patch   map[string]any `json:"patch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	if req.Patch == nil || len(req.Patch) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch is required")
		return
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

	result, err := opts.primitiveStore.PatchThread(r.Context(), actorID, threadID, req.Patch)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
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
		Tag:      strings.TrimSpace(query.Get("tag")),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list threads")
		return
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

	writeJSON(w, http.StatusOK, map[string]any{"events": events})
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
