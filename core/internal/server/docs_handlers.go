package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
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

	includeTombstoned := strings.TrimSpace(r.URL.Query().Get("include_tombstoned")) == "true"
	threadID := strings.TrimSpace(r.URL.Query().Get("thread_id"))
	documents, err := opts.primitiveStore.ListDocuments(r.Context(), primitives.DocumentListFilter{
		ThreadID:          threadID,
		IncludeTombstoned: includeTombstoned,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list documents")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"documents": documents,
	})
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
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

	document, revision, err := opts.primitiveStore.CreateDocument(r.Context(), actorID, req.Document, req.Content, req.ContentType, refs)
	if err != nil {
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
	if err := refreshDerivedThreadProjection(r.Context(), opts, anyString(document["thread_id"]), time.Now().UTC(), actorID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to refresh derived thread views")
		return
	}

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

func handleUpdateDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
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

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	document, revision, err := opts.primitiveStore.UpdateDocument(
		r.Context(),
		actorID,
		documentID,
		req.Document,
		req.IfBaseRevision,
		req.Content,
		req.ContentType,
		refs,
	)
	if err != nil {
		switch {
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
	if err := refreshDerivedThreadProjection(r.Context(), opts, anyString(document["thread_id"]), time.Now().UTC(), actorID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to refresh derived thread views")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
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

func handleTombstoneDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	document, revision, err := opts.primitiveStore.TombstoneDocument(r.Context(), actorID, documentID, req.Reason)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		if errors.Is(err, primitives.ErrInvalidDocumentRequest) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to tombstone document")
		return
	}
	if err := refreshDerivedThreadProjection(r.Context(), opts, anyString(document["thread_id"]), time.Now().UTC(), actorID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to refresh derived thread views")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"document": document,
		"revision": revision,
	})
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
