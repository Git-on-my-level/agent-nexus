package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

func handleAppendEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
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
		Event      map[string]any `json:"event"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if req.Event == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "event is required")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	if strings.TrimSpace(req.RequestKey) != "" && firstNonEmptyString(req.Event["id"]) == "" {
		req.Event["id"] = deriveRequestScopedID("events.create", actorID, req.RequestKey, "event")
	}
	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "events.create", actorID, req.RequestKey, req)
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
	typeValue, ok := req.Event["type"].(string)
	if !ok || strings.TrimSpace(typeValue) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "event.type is required")
		return
	}

	if err := schema.ValidateEnum(opts.contract, "event_type", typeValue); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if _, ok := req.Event["summary"].(string); !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "event.summary is required")
		return
	}

	refs, err := extractStringSlice(req.Event["refs"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "event.refs must be a list of strings")
		return
	}

	if err := schema.ValidateTypedRefs(opts.contract, refs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	provenance, ok := req.Event["provenance"].(map[string]any)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "event.provenance is required")
		return
	}

	if err := schema.ValidateProvenance(opts.contract, provenance); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := validateEventReferenceConventions(opts.contract, req.Event, refs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	stored, err := opts.primitiveStore.AppendEvent(r.Context(), actorID, req.Event)
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" {
			eventID := firstNonEmptyString(req.Event["id"])
			existing, loadErr := opts.primitiveStore.GetEvent(r.Context(), eventID)
			if loadErr == nil {
				response := map[string]any{"event": existing}
				status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "events.create", actorID, req.RequestKey, req, http.StatusCreated, response)
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
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "event already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to append event")
		return
	}
	enqueueThreadProjectionsBestEffort(r.Context(), opts, []string{anyString(stored["thread_id"])}, time.Now().UTC())

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "events.create", actorID, req.RequestKey, req, http.StatusCreated, map[string]any{"event": stored})
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, payload)
}

func handleGetEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, eventID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	event, err := opts.primitiveStore.GetEvent(r.Context(), eventID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load event")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"event": event})
}

func handleCreateArtifact(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
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
		Artifact    map[string]any `json:"artifact"`
		Content     any            `json:"content"`
		ContentType string         `json:"content_type"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if req.Artifact == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "artifact is required")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	kind, ok := req.Artifact["kind"].(string)
	if !ok || strings.TrimSpace(kind) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "artifact.kind is required")
		return
	}

	if err := schema.ValidateEnum(opts.contract, "artifact_kind", kind); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	refs, err := extractStringSlice(req.Artifact["refs"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "artifact.refs must be a list of strings")
		return
	}
	if err := schema.ValidateTypedRefs(opts.contract, refs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	req.ContentType = strings.TrimSpace(req.ContentType)
	if req.ContentType == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "content_type is required")
		return
	}
	if packetSchema, isPacketKind := opts.contract.Packets[kind]; isPacketKind {
		if packetSchema.Kind != "" && packetSchema.Kind != kind {
			writeError(w, http.StatusBadRequest, "invalid_request", "artifact.kind does not match packet schema")
			return
		}
		if req.ContentType != "structured" {
			writeError(w, http.StatusBadRequest, "invalid_request", "packet artifacts must use content_type=structured")
			return
		}
		packet, ok := req.Content.(map[string]any)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "packet artifacts must provide content as a JSON object")
			return
		}
		if _, err := validatePacketArtifactAndContent(opts.contract, kind, req.Artifact, packet); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}

	artifact, err := opts.primitiveStore.CreateArtifact(r.Context(), actorID, req.Artifact, req.Content, req.ContentType)
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "artifact already exists")
			return
		}
		if errors.Is(err, primitives.ErrInvalidArtifactID) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create artifact")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"artifact": artifact})
}

func handleGetArtifact(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifactID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	artifact, err := opts.primitiveStore.GetArtifact(r.Context(), artifactID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "artifact not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load artifact")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifact})
}

func handleGetArtifactContent(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifactID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	content, contentType, err := opts.primitiveStore.GetArtifactContent(r.Context(), artifactID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "artifact content not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load artifact content")
		return
	}

	switch contentType {
	case "structured":
		w.Header().Set("Content-Type", "application/json")
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	case "binary":
		w.Header().Set("Content-Type", "application/octet-stream")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func handleGetUsageSummary(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	summary, err := opts.primitiveStore.GetWorkspaceUsageSummary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load workspace usage summary")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"summary": summary})
}

func handleRebuildBlobUsageLedger(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	result, err := opts.primitiveStore.RebuildBlobUsageLedger(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to rebuild blob usage ledger")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"rebuild": result})
}

func handleListArtifacts(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	query := r.URL.Query()
	threadID := strings.TrimSpace(query.Get("thread_id"))
	if threadID == "" {
		threadID = strings.TrimSpace(query.Get("thread"))
	}

	includeTombstoned := strings.TrimSpace(query.Get("include_tombstoned")) == "true"

	var limitPtr *int
	if limitStr := strings.TrimSpace(query.Get("limit")); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limitPtr = &n
		}
	}

	artifacts, err := opts.primitiveStore.ListArtifacts(r.Context(), primitives.ArtifactListFilter{
		Q:                 strings.TrimSpace(query.Get("q")),
		Limit:             limitPtr,
		Kind:              strings.TrimSpace(query.Get("kind")),
		ThreadID:          threadID,
		CreatedBefore:     strings.TrimSpace(query.Get("created_before")),
		CreatedAfter:      strings.TrimSpace(query.Get("created_after")),
		IncludeTombstoned: includeTombstoned,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list artifacts")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts})
}

func handleGetSnapshot(w http.ResponseWriter, r *http.Request, opts handlerOptions, snapshotID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	snapshot, err := opts.primitiveStore.GetSnapshot(r.Context(), snapshotID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "snapshot not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load snapshot")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"snapshot": snapshot})
}

func requireRegisteredActorID(w http.ResponseWriter, r *http.Request, actorRegistry ActorRegistry, actorID string) (string, bool) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor_id is required")
		return "", false
	}

	if actorRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "actor_registry_unavailable", "actor registry is not configured")
		return "", false
	}

	exists, err := actorRegistry.Exists(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to validate actor_id")
		return "", false
	}
	if !exists {
		writeError(w, http.StatusBadRequest, "unknown_actor_id", "actor_id is not registered")
		return "", false
	}

	return actorID, true
}

func extractStringSlice(raw any) ([]string, error) {
	switch values := raw.(type) {
	case []string:
		out := make([]string, len(values))
		copy(out, values)
		return out, nil
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, errors.New("list contains non-string values")
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, errors.New("must be a list of strings")
	}
}

func handleTombstoneArtifact(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifactID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID string `json:"actor_id"`
		Reason  string `json:"reason"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	artifact, err := opts.primitiveStore.TombstoneArtifact(r.Context(), actorID, artifactID, req.Reason)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "artifact not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to tombstone artifact")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifact})
}
