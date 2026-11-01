// HTTP handlers for document CRUD reads and writes: list, create, get, and
// patch. Revision creation and full-content update/PUT live in docs_update.go,
// history reads in docs_revisions.go, lifecycle in docs_lifecycle.go, search
// in docs_search.go, and comments in docs_comments.go.

package server

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func writeInvalidDocumentRequest(w http.ResponseWriter, err error) {
	body := errorPayload("invalid_request", err.Error())
	var report *primitives.VisualReportValidationError
	if errors.As(err, &report) && len(report.Errors) > 0 {
		body["details"] = map[string]any{"errors": report.Errors}
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": body})
}

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

	query := r.URL.Query()
	states, parseErr := ParseListLifecycleStates(query)
	if parseErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", parseErr.Error())
		return
	}
	threadID := strings.TrimSpace(query.Get("thread_id"))
	if threadID != "" {
		resolved, ok := resolveListFilterResourceRef(w, r, opts, "thread", threadID, "thread_id")
		if !ok {
			return
		}
		threadID = resolved.ID
	}
	knowledge, knowledgeOK := parseOptionalBoolQuery(w, query.Get("knowledge"), "knowledge")
	if !knowledgeOK {
		return
	}
	documents, nextCursor, err := opts.primitiveStore.ListDocuments(r.Context(), primitives.DocumentListFilter{
		States:    states,
		ThreadID:  threadID,
		Query:     strings.TrimSpace(query.Get("q")),
		Tag:       strings.TrimSpace(query.Get("tag")),
		Knowledge: knowledge,
		Limit:     limitFilter,
		Cursor:    strings.TrimSpace(query.Get("cursor")),
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
		ActorID       string         `json:"actor_id"`
		RequestKey    string         `json:"request_key"`
		Document      map[string]any `json:"document"`
		Content       any            `json:"content"`
		ContentBase64 *string        `json:"content_base64"`
		ContentType   string         `json:"content_type"`
		Refs          any            `json:"refs"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Document == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "document is required")
		return
	}
	if req.Content == nil && req.ContentBase64 == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "content is required")
		return
	}
	if err := validateDocumentContentType(req.ContentType); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.ContentBase64 != nil {
		if req.ContentType != "binary" || req.Content != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "content_base64 is supported only for binary content and cannot be combined with content")
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(*req.ContentBase64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "content_base64 must be valid standard base64")
			return
		}
		req.Content = decoded
	}
	if _, has := req.Document["labels"]; has {
		writeError(w, http.StatusBadRequest, "invalid_request", "document.labels is not supported")
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
		req.Document["document_id"] = deriveRequestScopedID("docs.create", actorID, req.RequestKey, "dc")
	}
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeMapString(w, "document.summary", req.Document, "summary") {
		return
	}
	if !normalizeAnyMarkdownContent(w, &hygiene, "content", req.ContentType, &req.Content) {
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

	replayCtx, finishPreload, ok := preloadReplayDocumentContent(w, r.Context(), opts.primitiveStore, req.RequestKey, req.Content, req.ContentType)
	if !ok {
		return
	}
	status, response, err := executeAtomicMutation(replayCtx, opts.primitiveStore, "docs.create", actorID, req.RequestKey, req, http.StatusCreated, func(ctx context.Context) (map[string]any, error) {
		document, revision, err := opts.primitiveStore.CreateDocument(ctx, actorID, req.Document, req.Content, req.ContentType, refs)
		if err != nil {
			return nil, err
		}
		return hygiene.attach(map[string]any{"document": document, "revision": revision}), nil
	})
	finishPreload()
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "document already exists")
			return
		}
		if errors.Is(err, primitives.ErrInvalidDocumentRequest) {
			writeInvalidDocumentRequest(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create document")
		return
	}
	notifyProjectionMaintainer(opts)

	writeJSON(w, status, response)
}

func handleGetDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	documentID, ok = resolveHTTPResourceID(w, r, opts, "document", documentID, "document")
	if !ok {
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

func handlePatchDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	documentID, ok = resolveHTTPResourceID(w, r, opts, "document", documentID, "document")
	if !ok {
		return
	}
	var req struct {
		RequestKey  string         `json:"request_key"`
		ActorID     string         `json:"actor_id"`
		Patch       map[string]any `json:"patch"`
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
		ifUpdatedAt, ok := normalizeRequiredTimestamp(w, req.IfUpdatedAt, "if_updated_at")
		if !ok {
			return
		}
		req.IfUpdatedAt = &ifUpdatedAt
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeMapString(w, "patch.summary", req.Patch, "summary") {
		return
	}
	status, response, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "docs.patch", actorID, req.RequestKey, struct {
		ID   string
		Body any
	}{documentID, req}, http.StatusOK, func(ctx context.Context) (map[string]any, error) {
		document, revision, err := opts.primitiveStore.PatchDocument(ctx, actorID, documentID, req.Patch, req.IfUpdatedAt)
		if err != nil {
			return nil, err
		}
		return hygiene.attach(map[string]any{"document": document, "revision": revision}), nil
	})
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		switch {
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "document not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "document has been updated; refresh and retry")
		case errors.Is(err, primitives.ErrInvalidDocumentRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to patch document")
		}
		return
	}
	notifyProjectionMaintainer(opts)
	writeJSON(w, status, response)
}
