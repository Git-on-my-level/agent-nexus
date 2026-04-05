package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

func handleListDocuments(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var limitFilter *int
	limitRaw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 1000")
			return
		}
		limitFilter = &parsed
	}

	includeTrashed := strings.TrimSpace(r.URL.Query().Get("include_trashed")) == "true"
	trashedOnly := strings.TrimSpace(r.URL.Query().Get("trashed_only")) == "true"
	includeArchived := strings.TrimSpace(r.URL.Query().Get("include_archived")) == "true"
	archivedOnly := strings.TrimSpace(r.URL.Query().Get("archived_only")) == "true"
	threadID := strings.TrimSpace(r.URL.Query().Get("thread_id"))
	documents, nextCursor, err := opts.primitiveStore.ListDocuments(r.Context(), primitives.DocumentListFilter{
		ThreadID:        threadID,
		IncludeTrashed:  includeTrashed,
		TrashedOnly:     trashedOnly,
		IncludeArchived: includeArchived,
		ArchivedOnly:    archivedOnly,
		Query:           strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:           limitFilter,
		Cursor:          strings.TrimSpace(r.URL.Query().Get("cursor")),
	})
	if err != nil {
		if errors.Is(err, primitives.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list documents")
		return
	}

	response := map[string]any{
		"documents": documents,
	}
	if nextCursor != "" {
		response["next_cursor"] = nextCursor
	}
	writeJSON(w, http.StatusOK, response)
}

func handleCreateDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
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
		RequestKey  string         `json:"request_key"`
		Document    map[string]any `json:"document"`
		Content     any            `json:"content"`
		ContentType string         `json:"content_type"`
		Refs        any            `json:"refs"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Document == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "document is required")
		return
	}
	if req.Content == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "content is required")
		return
	}
	if err := validateDocumentContentType(req.ContentType); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if docID := firstNonEmptyString(req.Document["document_id"], req.Document["id"]); docID != "" {
		if err := validateDocumentID(docID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	if strings.TrimSpace(req.RequestKey) != "" && firstNonEmptyString(req.Document["document_id"], req.Document["id"]) == "" {
		req.Document["document_id"] = deriveRequestScopedID("docs.create", actorID, req.RequestKey, "doc")
	}
	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "docs.create", actorID, req.RequestKey, req)
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
	refs, err := optionalRefs(req.Refs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := schema.ValidateTypedRefs(opts.contract, refs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if _, has := req.Document["refs"]; has {
		docRefs, derr := optionalRefs(req.Document["refs"])
		if derr != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", derr.Error())
			return
		}
		if err := schema.ValidateTypedRefs(opts.contract, docRefs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}

	document, revision, err := opts.primitiveStore.CreateDocument(r.Context(), actorID, req.Document, req.Content, req.ContentType, refs)
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" {
			documentID := firstNonEmptyString(req.Document["document_id"], req.Document["id"])
			existingDocument, existingRevision, loadErr := opts.primitiveStore.GetDocument(r.Context(), documentID)
			if loadErr == nil {
				response := map[string]any{
					"document": existingDocument,
					"revision": existingRevision,
				}
				status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "docs.create", actorID, req.RequestKey, req, http.StatusCreated, response)
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
			writeError(w, http.StatusConflict, "conflict", "document already exists")
			return
		}
		if errors.Is(err, primitives.ErrInvalidDocumentRequest) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create document")
		return
	}
	enqueueTopicProjectionsBestEffort(r.Context(), opts, []string{anyString(document["thread_id"])}, time.Now().UTC())

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "docs.create", actorID, req.RequestKey, req, http.StatusCreated, map[string]any{
		"document": document,
		"revision": revision,
	})
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, payload)
}

func handleGetDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if err := validateDocumentID(documentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	document, revision, err := opts.primitiveStore.GetDocument(r.Context(), documentID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"document": document,
		"revision": revision,
	})
}

// handleCreateDocumentRevision serves POST /docs/{document_id}/revisions.
// It accepts the OpenAPI CreateDocumentRevisionRequest shape (revision + optional if_document_updated_at)
// and the CLI/docs-update envelope (if_base_revision + content + content_type), matching PATCH /docs/{document_id}.
func handleCreateDocumentRevision(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if err := validateDocumentID(documentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "failed to read body")
		return
	}

	var probe map[string]any
	if err := json.Unmarshal(bodyBytes, &probe); err != nil || probe == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "JSON body must be an object")
		return
	}

	rebuild := bodyBytes
	if rev, ok := probe["revision"].(map[string]any); ok && len(rev) > 0 {
		if opts.contract == nil {
			writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
			return
		}
		document, _, err := opts.primitiveStore.GetDocument(r.Context(), documentID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "document not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document")
			return
		}
		if raw := strings.TrimSpace(anyString(probe["if_document_updated_at"])); raw != "" {
			if docUA := strings.TrimSpace(anyString(document["updated_at"])); docUA != "" && docUA != raw {
				writeError(w, http.StatusConflict, "conflict", "document has been updated; refresh and retry")
				return
			}
		}
		headID := strings.TrimSpace(anyString(document["head_revision_id"]))
		if headID == "" {
			writeError(w, http.StatusInternalServerError, "internal_error", "document has no head revision")
			return
		}
		baseRevision := headID
		if raw := strings.TrimSpace(anyString(probe["if_base_revision"])); raw != "" {
			baseRevision = raw
		}
		bodyMD := strings.TrimSpace(anyString(rev["body_markdown"]))
		if bodyMD == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "revision.body_markdown is required")
			return
		}
		provRaw, hasProv := rev["provenance"]
		if !hasProv {
			writeError(w, http.StatusBadRequest, "invalid_request", "revision.provenance is required")
			return
		}
		prov, ok := provRaw.(map[string]any)
		if !ok || prov == nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "revision.provenance must be an object")
			return
		}
		refs, err := optionalRefs(rev["refs"])
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if err := schema.ValidateTypedRefs(opts.contract, refs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}

		synthetic := map[string]any{
			"actor_id":         probe["actor_id"],
			"if_base_revision": baseRevision,
			"content":          bodyMD,
			"content_type":     "text",
			"refs":             refs,
			"provenance":       prov,
		}
		if sum := strings.TrimSpace(anyString(rev["summary"])); sum != "" {
			synthetic["document"] = map[string]any{"title": sum}
		}
		rebuild, err = json.Marshal(synthetic)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to build revision request")
			return
		}
	}

	r.Body = io.NopCloser(bytes.NewReader(rebuild))
	r.ContentLength = int64(len(rebuild))
	r.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(rebuild)), nil
	}

	handleUpdateDocument(w, r, opts, documentID, http.StatusCreated)
}

func handleUpdateDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string, successStatus int) {
	if successStatus == 0 {
		successStatus = http.StatusOK
	}
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}
	if err := validateDocumentID(documentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	var req struct {
		ActorID        string         `json:"actor_id"`
		Document       map[string]any `json:"document"`
		IfBaseRevision string         `json:"if_base_revision"`
		Content        any            `json:"content"`
		ContentType    string         `json:"content_type"`
		Refs           any            `json:"refs"`
		Provenance     map[string]any `json:"provenance"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	req.IfBaseRevision = strings.TrimSpace(req.IfBaseRevision)
	if req.IfBaseRevision == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "if_base_revision is required")
		return
	}
	if req.Content == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "content is required")
		return
	}
	if err := validateDocumentContentType(req.ContentType); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	refs, err := optionalRefs(req.Refs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := schema.ValidateTypedRefs(opts.contract, refs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Document != nil {
		if _, has := req.Document["refs"]; has {
			docRefs, derr := optionalRefs(req.Document["refs"])
			if derr != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", derr.Error())
				return
			}
			if err := schema.ValidateTypedRefs(opts.contract, docRefs); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
		}
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	previousDocument, _, err := opts.primitiveStore.GetDocument(r.Context(), documentID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document")
		return
	}
	previousThreadID := anyString(previousDocument["thread_id"])

	document, revision, err := opts.primitiveStore.UpdateDocument(
		r.Context(),
		actorID,
		documentID,
		req.Document,
		req.IfBaseRevision,
		req.Content,
		req.ContentType,
		refs,
		req.Provenance,
	)
	if err != nil {
		switch {
		case writePrimitiveQuotaViolationError(w, err):
			return
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "document not found")
		case errors.Is(err, primitives.ErrInvalidDocumentRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "document has been updated; refresh and retry")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to update document")
		}
		return
	}
	refreshNow := time.Now().UTC()
	threadIDsToRefresh := map[string]struct{}{
		previousThreadID:                 {},
		anyString(document["thread_id"]): {},
	}
	threadIDs := make([]string, 0, len(threadIDsToRefresh))
	for threadID := range threadIDsToRefresh {
		threadIDs = append(threadIDs, threadID)
	}
	enqueueTopicProjectionsBestEffort(r.Context(), opts, threadIDs, refreshNow)

	writeJSON(w, successStatus, map[string]any{
		"document": document,
		"revision": revision,
	})
}

func handleListDocumentHistory(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if err := validateDocumentID(documentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	history, err := opts.primitiveStore.ListDocumentHistory(r.Context(), documentID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document history")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"document_id": documentID,
		"revisions":   history,
	})
}

func handleGetDocumentRevision(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string, revisionID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if err := validateDocumentID(documentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	revisionID = strings.TrimSpace(revisionID)
	if revisionID == "" || strings.Contains(revisionID, "/") {
		writeError(w, http.StatusBadRequest, "invalid_request", "revision_id is required")
		return
	}

	revision, err := opts.primitiveStore.GetDocumentRevision(r.Context(), documentID, revisionID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document revision not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document revision")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"revision": revision,
	})
}

func validateDocumentID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("document_id is required")
	}
	if filepath.IsAbs(value) {
		return errors.New("document_id must not be absolute")
	}
	if value == "." || value == ".." {
		return errors.New("document_id must not be . or ..")
	}
	if strings.Contains(value, "/") || strings.Contains(value, `\`) {
		return errors.New("document_id must not contain path separators")
	}
	return nil
}

func validateDocumentContentType(contentType string) error {
	switch strings.TrimSpace(contentType) {
	case "text", "structured", "binary":
		return nil
	default:
		return errors.New("content_type must be one of: text, structured, binary")
	}
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		text, _ := value.(string)
		text = strings.TrimSpace(text)
		if text != "" {
			return text
		}
	}
	return ""
}

func handleTrashDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if err := validateDocumentID(documentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
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

	document, revision, err := opts.primitiveStore.TrashDocument(r.Context(), actorID, documentID, req.Reason)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		if errors.Is(err, primitives.ErrInvalidDocumentRequest) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to trash document")
		return
	}
	enqueueTopicProjectionsBestEffort(r.Context(), opts, []string{anyString(document["thread_id"])}, time.Now().UTC())

	writeJSON(w, http.StatusOK, map[string]any{
		"document": document,
		"revision": revision,
	})
}

func handleArchiveDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if err := validateDocumentID(documentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
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

	document, revision, err := opts.primitiveStore.ArchiveDocument(r.Context(), actorID, documentID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		if errors.Is(err, primitives.ErrAlreadyTrashed) {
			writeError(w, http.StatusConflict, "already_trashed", "document is trashed")
			return
		}
		if errors.Is(err, primitives.ErrInvalidDocumentRequest) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to archive document")
		return
	}
	enqueueTopicProjectionsBestEffort(r.Context(), opts, []string{anyString(document["thread_id"])}, time.Now().UTC())

	writeJSON(w, http.StatusOK, map[string]any{
		"document": document,
		"revision": revision,
	})
}

func handleUnarchiveDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if err := validateDocumentID(documentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
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

	document, revision, err := opts.primitiveStore.UnarchiveDocument(r.Context(), actorID, documentID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		if errors.Is(err, primitives.ErrNotArchived) {
			writeError(w, http.StatusConflict, "not_archived", "document is not archived")
			return
		}
		if errors.Is(err, primitives.ErrInvalidDocumentRequest) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to unarchive document")
		return
	}
	enqueueTopicProjectionsBestEffort(r.Context(), opts, []string{anyString(document["thread_id"])}, time.Now().UTC())

	writeJSON(w, http.StatusOK, map[string]any{
		"document": document,
		"revision": revision,
	})
}

func handleRestoreDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if err := validateDocumentID(documentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
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

	document, revision, err := opts.primitiveStore.RestoreDocument(r.Context(), actorID, documentID, req.Reason)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		if errors.Is(err, primitives.ErrNotTrashed) {
			writeError(w, http.StatusConflict, "not_trashed", "document is not currently trashed")
			return
		}
		if errors.Is(err, primitives.ErrInvalidDocumentRequest) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to restore document")
		return
	}
	enqueueTopicProjectionsBestEffort(r.Context(), opts, []string{anyString(document["thread_id"])}, time.Now().UTC())

	writeJSON(w, http.StatusOK, map[string]any{
		"document": document,
		"revision": revision,
	})
}

func handlePurgeDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if err := validateDocumentID(documentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isHumanPrincipal(principal) {
		writeError(w, http.StatusForbidden, "human_only", "only human principals may permanently delete documents")
		return
	}

	err := opts.primitiveStore.PurgeDocument(r.Context(), documentID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		if errors.Is(err, primitives.ErrNotTrashed) {
			writeError(w, http.StatusConflict, "not_trashed", "document is not currently trashed")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to permanently delete document")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"purged": true, "document_id": documentID})
}

func optionalRefs(raw any) ([]string, error) {
	if raw == nil {
		return []string{}, nil
	}
	refs, err := extractStringSlice(raw)
	if err != nil {
		return nil, errors.New("refs must be a list of strings")
	}
	return refs, nil
}
