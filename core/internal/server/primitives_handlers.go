package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
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
		req.Event["id"] = deriveRequestScopedID("events.create", actorID, req.RequestKey, "ev")
	}
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeMapString(w, "event.summary", req.Event, "summary") {
		return
	}
	if strings.TrimSpace(anyString(req.Event["type"])) == "message_posted" {
		if payload, ok := req.Event["payload"].(map[string]any); ok {
			if !hygiene.normalizeMapString(w, "event.payload.text", payload, "text") {
				return
			}
		}
	}
	if strings.TrimSpace(anyString(req.Event["type"])) == "human_attention_requested" {
		if payload, ok := req.Event["payload"].(map[string]any); ok {
			if !hygiene.normalizeMapString(w, "event.payload.body", payload, "body") {
				return
			}
		}
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
				status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "events.create", actorID, req.RequestKey, req, http.StatusCreated, hygiene.attach(response))
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
	threadID := anyString(stored["thread_id"])
	enqueueTopicProjectionsBestEffort(r.Context(), opts, []string{threadID}, time.Now().UTC())
	if strings.TrimSpace(anyString(stored["type"])) == "human_attention_requested" && opts.projectionMaintainer != nil {
		_ = opts.projectionMaintainer.RefreshThread(r.Context(), threadID, time.Now().UTC())
	}

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "events.create", actorID, req.RequestKey, req, http.StatusCreated, hygiene.attach(map[string]any{"event": stored}))
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
	resolvedID, ok := resolveHTTPResourceID(w, r, opts, "event", eventID, "event")
	if !ok {
		return
	}

	event, err := opts.primitiveStore.GetEvent(r.Context(), resolvedID)
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

func handleArchiveEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, eventID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	event, err := opts.primitiveStore.ArchiveEvent(r.Context(), actorID, eventID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		if errors.Is(err, primitives.ErrAlreadyTrashed) {
			writeError(w, http.StatusConflict, "already_trashed", "event is trashed")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to archive event")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"event": event})
}

func handleUnarchiveEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, eventID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	event, err := opts.primitiveStore.UnarchiveEvent(r.Context(), actorID, eventID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		if errors.Is(err, primitives.ErrNotArchived) {
			writeError(w, http.StatusConflict, "not_archived", "event is not archived")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to unarchive event")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"event": event})
}

func handleTrashEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, eventID string) {
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

	event, err := opts.primitiveStore.TrashEvent(r.Context(), actorID, eventID, req.Reason)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to trash event")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"event": event})
}

func handleRestoreEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, eventID string) {
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

	event, err := opts.primitiveStore.RestoreEvent(r.Context(), actorID, eventID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		if errors.Is(err, primitives.ErrNotTrashed) {
			writeError(w, http.StatusConflict, "not_trashed", "event is not currently trashed")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to restore event")
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
	switch strings.TrimSpace(kind) {
	case "doc", "card":
		writeError(w, http.StatusBadRequest, "invalid_request", "doc and card artifacts are created only by revision endpoints")
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

func handleCreateArtifactAttachment(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "attachment exceeds maximum upload size")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid multipart form")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "file part is required")
		return
	}
	defer func() { _ = file.Close() }()

	declared := header.Header.Get("Content-Type")
	br := bufio.NewReader(file)
	peek, _ := br.Peek(512)
	detected := http.DetectContentType(peek)
	mimeChosen, err := primitives.ChooseAttachmentMIME(declared, detected)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unsupported_mime", err.Error())
		return
	}

	refsRaw := strings.TrimSpace(r.FormValue("refs"))
	if refsRaw == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "refs is required")
		return
	}
	var refs []string
	if err := json.Unmarshal([]byte(refsRaw), &refs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "refs must be a JSON array of strings")
		return
	}
	if err := schema.ValidateTypedRefs(opts.contract, refs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	artifact := map[string]any{
		"kind": "attachment",
		"refs": refs,
	}
	if summary := strings.TrimSpace(r.FormValue("summary")); summary != "" {
		artifact["summary"] = summary
	}
	if rawExtras := strings.TrimSpace(r.FormValue("artifact")); rawExtras != "" {
		var extras map[string]any
		if err := json.Unmarshal([]byte(rawExtras), &extras); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "artifact must be a JSON object when provided")
			return
		}
		for k, v := range extras {
			if k == "refs" || k == "kind" {
				continue
			}
			artifact[k] = v
		}
	}

	actorID, ok := resolveWriteActorID(w, r, opts, r.FormValue("actor_id"))
	if !ok {
		return
	}

	maxBytes := opts.requestBodyLimits.normalize().Attachment
	created, err := opts.primitiveStore.CreateArtifactAttachment(r.Context(), actorID, artifact, mimeChosen, header.Filename, br, maxBytes)
	if err != nil {
		if errors.Is(err, blob.ErrUploadTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "attachment exceeds maximum upload size")
			return
		}
		if errors.Is(err, primitives.ErrAttachmentMimeNotAllowed) {
			writeError(w, http.StatusBadRequest, "unsupported_mime", err.Error())
			return
		}
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
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create attachment")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"artifact": created})
}

func handleGetArtifact(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifactID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	resolvedID, ok := resolveHTTPResourceID(w, r, opts, "artifact", artifactID, "artifact")
	if !ok {
		return
	}

	artifact, err := opts.primitiveStore.GetArtifact(r.Context(), resolvedID)
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

func handleRestoreArtifact(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifactID string) {
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

	artifact, err := opts.primitiveStore.RestoreArtifact(r.Context(), actorID, artifactID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "artifact not found")
			return
		}
		if errors.Is(err, primitives.ErrNotTrashed) {
			writeError(w, http.StatusConflict, "not_trashed", "artifact is not currently trashed")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to restore artifact")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifact})
}

func actorRegistryActorHasHumanTag(ctx context.Context, registry ActorRegistry, actorID string) bool {
	if registry == nil || strings.TrimSpace(actorID) == "" {
		return false
	}
	act, err := registry.Get(ctx, actorID)
	if err != nil {
		return false
	}
	for _, tag := range act.Tags {
		if strings.EqualFold(strings.TrimSpace(tag), "human") {
			return true
		}
	}
	return false
}

func handlePurgeArtifact(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifactID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	principal, ok := resolveOptionalPrincipal(w, r, opts)
	if !ok {
		return
	}

	if principal != nil {
		if !isHumanPrincipal(principal) {
			writeError(w, http.StatusForbidden, "human_only", "only human principals may permanently delete artifacts")
			return
		}
	} else {
		if !opts.allowUnauthenticatedWrites || !opts.enableDevActorMode {
			writeError(w, http.StatusUnauthorized, "auth_required", "authorization header is required")
			return
		}
		if opts.actorRegistry == nil {
			writeError(w, http.StatusServiceUnavailable, "actor_registry_unavailable", "actor registry is not configured")
			return
		}
		var req struct {
			ActorID string `json:"actor_id"`
			Reason  string `json:"reason"`
		}
		if !decodeJSONBodyAllowEmpty(w, r, &req) {
			return
		}
		actorID := strings.TrimSpace(req.ActorID)
		if actorID == "" {
			writeError(w, http.StatusForbidden, "human_only", "only human principals may permanently delete artifacts; in development, include actor_id for an actor tagged `human` in the JSON body")
			return
		}
		registeredID, ok := requireRegisteredActorID(w, r, opts.actorRegistry, actorID)
		if !ok {
			return
		}
		if !actorRegistryActorHasHumanTag(r.Context(), opts.actorRegistry, registeredID) {
			writeError(w, http.StatusForbidden, "human_only", "only human-tagged actors may permanently delete without authenticated passkey credentials")
			return
		}
	}

	err := opts.primitiveStore.PurgeTrashedArtifact(r.Context(), artifactID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "artifact not found")
			return
		}
		if errors.Is(err, primitives.ErrNotTrashed) {
			writeError(w, http.StatusConflict, "not_trashed", "artifact is not currently trashed")
			return
		}
		if errors.Is(err, primitives.ErrOwnedArtifactLifecycle) {
			writeError(w, http.StatusConflict, "owned_artifact_lifecycle", "artifact lifecycle is owned by its parent resource")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to permanently delete artifact")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"purged": true, "artifact_id": artifactID})
}

func handleGetArtifactContent(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifactID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	resolvedID, ok := resolveHTTPResourceID(w, r, opts, "artifact", artifactID, "artifact")
	if !ok {
		return
	}
	delivery, err := opts.primitiveStore.GetArtifactContentHTTP(r.Context(), resolvedID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "artifact content not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load artifact content")
		return
	}

	if delivery.ContentType != "" {
		w.Header().Set("Content-Type", delivery.ContentType)
	}
	if delivery.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", delivery.ContentDisposition)
	}
	if delivery.ETag != "" {
		w.Header().Set("ETag", delivery.ETag)
	}
	if delivery.LastModified != "" {
		w.Header().Set("Last-Modified", delivery.LastModified)
	}
	if delivery.CacheControl != "" {
		w.Header().Set("Cache-Control", delivery.CacheControl)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if delivery.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(delivery.ContentLength, 10))
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(delivery.Body)
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

func handleGetUsageV1Summary(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	summary, err := opts.primitiveStore.GetWorkspaceUsageV1Summary(r.Context())
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
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	query := r.URL.Query()
	threadID := strings.TrimSpace(query.Get("thread_id"))
	var threadIDs []string
	if threadID != "" {
		resolved, ok := resolveListFilterResourceRef(w, r, opts, "thread", threadID, "thread_id")
		if !ok {
			return
		}
		threadID = resolved.ID
		threadIDs = resolvedRefStorageCandidates(resolved)
	}

	var artifactIDs []string
	if idsCSV := strings.TrimSpace(query.Get("ids")); idsCSV != "" {
		parts := strings.Split(idsCSV, ",")
		raw := make([]string, 0, len(parts))
		for _, part := range parts {
			if id := strings.TrimSpace(part); id != "" {
				raw = append(raw, id)
			}
		}
		artifactIDs = make([]string, 0, len(raw))
		for _, rawID := range primitives.NormalizeArtifactIDFilter(raw, 48) {
			resolved, ok := resolveListFilterResourceRef(w, r, opts, "artifact", rawID, "ids")
			if !ok {
				return
			}
			artifactIDs = append(artifactIDs, resolved.ID)
		}
		artifactIDs = primitives.NormalizeArtifactIDFilter(artifactIDs, 48)
	}

	states, parseErr := ParseListLifecycleStates(query)
	if parseErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", parseErr.Error())
		return
	}

	var limitPtr *int
	if limitStr := strings.TrimSpace(query.Get("limit")); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limitPtr = &n
		}
	}
	kind := strings.TrimSpace(query.Get("kind"))
	if kind != "" {
		if err := schema.ValidateEnum(opts.contract, "artifact_kind", kind); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	backingScope, ok := parseBackingScope(w, opts, query.Get("backing_scope"))
	if !ok {
		return
	}

	listFilter := primitives.ArtifactListFilter{
		States:        states,
		Q:             strings.TrimSpace(query.Get("q")),
		Limit:         limitPtr,
		Kind:          kind,
		BackingScope:  backingScope,
		ThreadID:      threadID,
		ThreadIDs:     threadIDs,
		CreatedBefore: strings.TrimSpace(query.Get("created_before")),
		CreatedAfter:  strings.TrimSpace(query.Get("created_after")),
	}
	if len(artifactIDs) > 0 {
		listFilter.IDs = artifactIDs
		listFilter.ThreadID = ""
	}

	artifacts, err := opts.primitiveStore.ListArtifacts(r.Context(), listFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list artifacts")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts})
}

func requireRegisteredActorID(w http.ResponseWriter, r *http.Request, actorRegistry ActorRegistry, actorID string) (string, bool) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor_id is required")
		return "", false
	}
	if actors.IsReservedServiceActorID(actorID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor_id is reserved for system use")
		return "", false
	}

	if actorRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "actor_registry_unavailable", "actor registry is not configured")
		return "", false
	}

	exists, err := actorRegistry.Exists(r.Context(), actorID)
	if err != nil {
		log.Printf("anx-core: actor registry Exists failed for %q: %v", actorID, err)
		if actorExistsDebugErrors() {
			writeError(w, http.StatusInternalServerError, "internal_error", fmt.Sprintf("failed to validate actor_id: %v", err))
			return "", false
		}
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

func handleTrashArtifact(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifactID string) {
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

	artifact, err := opts.primitiveStore.TrashArtifact(r.Context(), actorID, artifactID, req.Reason)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "artifact not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to trash artifact")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifact})
}

func handleArchiveArtifact(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifactID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	artifact, err := opts.primitiveStore.ArchiveArtifact(r.Context(), actorID, artifactID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "artifact not found")
			return
		}
		if errors.Is(err, primitives.ErrAlreadyTrashed) {
			writeError(w, http.StatusConflict, "already_trashed", "artifact is trashed")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to archive artifact")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifact})
}

func handleUnarchiveArtifact(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifactID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	artifact, err := opts.primitiveStore.UnarchiveArtifact(r.Context(), actorID, artifactID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "artifact not found")
			return
		}
		if errors.Is(err, primitives.ErrNotArchived) {
			writeError(w, http.StatusConflict, "not_archived", "artifact is not archived")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to unarchive artifact")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"artifact": artifact})
}
