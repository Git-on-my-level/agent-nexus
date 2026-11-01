// HTTP handlers for the artifact primitive: create (inline content and
// attachments), get, and the restore/trash/archive/unarchive/purge lifecycle,
// including the human-tag check that gates destructive artifact operations.

package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/schema"
)

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

	if !authorizeResourceValues(w, r, artifact) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, r.FormValue("actor_id"))
	if !ok {
		return
	}

	maxBytes := opts.requestBodyLimits.normalize().Attachment
	key := strings.TrimSpace(r.FormValue("request_key"))
	if len(key) > 256 || strings.ContainsRune(key, 0) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid upload request key")
		return
	}
	var created map[string]any
	if key != "" {
		replayStore, ok := opts.primitiveStore.(interface {
			CreateArtifactAttachmentWithReplay(context.Context, string, map[string]any, string, string, io.Reader, int64, string) (map[string]any, error)
		})
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "replay_unavailable", "safe upload retry is unavailable")
			return
		}
		created, err = replayStore.CreateArtifactAttachmentWithReplay(r.Context(), actorID, artifact, mimeChosen, header.Filename, br, maxBytes, key)
	} else {
		created, err = opts.primitiveStore.CreateArtifactAttachment(r.Context(), actorID, artifact, mimeChosen, header.Filename, br, maxBytes)
	}
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "resource not found")
			return
		}
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
		if errors.Is(err, resourceaccess.ErrNULText) {
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
	if !requireAccessibleArtifact(w, r, opts, artifact) {
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
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
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
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
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
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
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
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
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
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
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
