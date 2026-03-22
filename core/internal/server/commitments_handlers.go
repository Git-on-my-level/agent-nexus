package server

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

func handleCreateCommitment(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID    string         `json:"actor_id"`
		RequestKey string         `json:"request_key"`
		Commitment map[string]any `json:"commitment"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if req.Commitment == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "commitment is required")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	if strings.TrimSpace(req.RequestKey) != "" && firstNonEmptyString(req.Commitment["id"]) == "" {
		req.Commitment["id"] = deriveRequestScopedID("commitments.create", actorID, req.RequestKey, "commitment")
	}
	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "commitments.create", actorID, req.RequestKey, req)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load idempotency replay")
		return
	}
	if replayed {
		writeJSON(w, replayStatus, replayPayload)
		return
	}
	if err := validateCommitmentCreate(opts.contract, req.Commitment); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := opts.primitiveStore.CreateCommitment(r.Context(), actorID, req.Commitment)
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" {
			commitmentID := firstNonEmptyString(req.Commitment["id"])
			commitment, loadErr := opts.primitiveStore.GetCommitment(r.Context(), commitmentID)
			if loadErr == nil {
				response := map[string]any{"commitment": commitment}
				status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "commitments.create", actorID, req.RequestKey, req, http.StatusCreated, response)
				if writeIdempotencyError(w, replayErr) {
					return
				}
				if replayErr != nil {
					writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
					return
				}
				writeJSON(w, status, payload)
				return
			}
		}
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
			return
		}
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "commitment already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create commitment")
		return
	}
	enqueueThreadProjectionsBestEffort(r.Context(), opts, []string{anyString(result.Snapshot["thread_id"])}, time.Now().UTC())

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "commitments.create", actorID, req.RequestKey, req, http.StatusCreated, map[string]any{"commitment": result.Snapshot})
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, payload)
}

func handleGetCommitment(w http.ResponseWriter, r *http.Request, opts handlerOptions, commitmentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	commitment, err := opts.primitiveStore.GetCommitment(r.Context(), commitmentID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "commitment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load commitment")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"commitment": commitment})
}

func handlePatchCommitment(w http.ResponseWriter, r *http.Request, opts handlerOptions, commitmentID string) {
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
		Refs        []string       `json:"refs"`
		IfUpdatedAt *string        `json:"if_updated_at"`
	}
	if !decodeJSONBody(w, r, &req) {
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
	if _, has := req.Patch["thread_id"]; has {
		writeError(w, http.StatusBadRequest, "invalid_request", "commitment.thread_id cannot be patched")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	if err := validateCommitmentPatch(opts.contract, req.Patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := schema.ValidateTypedRefs(opts.contract, req.Refs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := opts.primitiveStore.PatchCommitment(r.Context(), actorID, commitmentID, req.Patch, req.Refs, req.IfUpdatedAt)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "commitment not found")
			return
		}
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "commitment has been updated; refresh and retry")
			return
		}
		if errors.Is(err, primitives.ErrInvalidCommitmentTransition) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to patch commitment")
		return
	}
	enqueueThreadProjectionsBestEffort(r.Context(), opts, []string{anyString(result.Snapshot["thread_id"])}, time.Now().UTC())

	writeJSON(w, http.StatusOK, map[string]any{"commitment": result.Snapshot})
}

func handleListCommitments(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	query := r.URL.Query()
	dueAfter := strings.TrimSpace(query.Get("due_after"))
	dueBefore := strings.TrimSpace(query.Get("due_before"))
	if dueAfter != "" {
		if _, err := time.Parse(time.RFC3339, dueAfter); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "due_after must be an RFC3339 datetime string")
			return
		}
	}
	if dueBefore != "" {
		if _, err := time.Parse(time.RFC3339, dueBefore); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "due_before must be an RFC3339 datetime string")
			return
		}
	}

	commitments, err := opts.primitiveStore.ListCommitments(r.Context(), primitives.CommitmentListFilter{
		ThreadID:  strings.TrimSpace(query.Get("thread_id")),
		Owner:     strings.TrimSpace(query.Get("owner")),
		Status:    strings.TrimSpace(query.Get("status")),
		DueAfter:  dueAfter,
		DueBefore: dueBefore,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list commitments")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"commitments": commitments})
}

func validateCommitmentCreate(contract *schema.Contract, commitment map[string]any) error {
	commitmentSchema, ok := contract.Snapshots["commitment"]
	if !ok {
		return fmt.Errorf("commitment schema is not loaded")
	}

	required := make([]string, 0)
	for name, field := range commitmentSchema.Fields {
		if field.Required {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	for _, name := range required {
		value, exists := commitment[name]
		if !exists {
			return fmt.Errorf("commitment.%s is required", name)
		}
		if err := validateCommitmentField(contract, name, value, true); err != nil {
			return err
		}
	}

	for name, value := range commitment {
		if err := validateCommitmentField(contract, name, value, true); err != nil {
			return err
		}
	}

	return nil
}

func validateCommitmentPatch(contract *schema.Contract, patch map[string]any) error {
	for name, value := range patch {
		if err := validateCommitmentField(contract, name, value, false); err != nil {
			return err
		}
	}
	return nil
}

func validateCommitmentField(contract *schema.Contract, fieldName string, value any, createMode bool) error {
	commitmentSchema, ok := contract.Snapshots["commitment"]
	if !ok {
		return fmt.Errorf("commitment schema is not loaded")
	}
	field, known := commitmentSchema.Fields[fieldName]
	if !known {
		// Unknown fields are allowed and preserved by patch/merge semantics.
		return nil
	}

	switch field.Type {
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("commitment.%s must be a string", fieldName)
		}
		if createMode && field.Required && strings.TrimSpace(text) == "" {
			return fmt.Errorf("commitment.%s must be non-empty", fieldName)
		}
		if strings.HasPrefix(field.Ref, "enums.") {
			enumName := strings.TrimPrefix(field.Ref, "enums.")
			if err := schema.ValidateEnum(contract, enumName, text); err != nil {
				return fmt.Errorf("commitment.%s: %w", fieldName, err)
			}
		}
	case "datetime":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("commitment.%s must be an RFC3339 datetime string", fieldName)
		}
		if _, err := time.Parse(time.RFC3339, text); err != nil {
			return fmt.Errorf("commitment.%s must be an RFC3339 datetime string", fieldName)
		}
	case "list<string>":
		values, err := extractStringSlice(value)
		if err != nil {
			return fmt.Errorf("commitment.%s must be a list of strings", fieldName)
		}
		if field.MinItems != nil && len(values) < *field.MinItems {
			return fmt.Errorf("commitment.%s must include at least %d item(s)", fieldName, *field.MinItems)
		}
	case "list<typed_ref>":
		refs, err := extractStringSlice(value)
		if err != nil {
			return fmt.Errorf("commitment.%s must be a list of strings", fieldName)
		}
		if field.MinItems != nil && len(refs) < *field.MinItems {
			return fmt.Errorf("commitment.%s must include at least %d item(s)", fieldName, *field.MinItems)
		}
		if err := schema.ValidateTypedRefs(contract, refs); err != nil {
			return fmt.Errorf("commitment.%s: %w", fieldName, err)
		}
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("commitment.%s must be an object", fieldName)
		}
		if field.Ref == "provenance" {
			if err := schema.ValidateProvenance(contract, obj); err != nil {
				return fmt.Errorf("commitment.%s: %w", fieldName, err)
			}
		}
	}

	return nil
}
