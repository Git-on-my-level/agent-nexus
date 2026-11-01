// Content-changing document writes: revision creation (both the OpenAPI
// CreateDocumentRevisionRequest and the CLI docs revise envelope), patch-style
// update, and PUT replace.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

// handleCreateDocumentRevision serves POST /docs/{document_id}/revisions.
// It accepts the OpenAPI CreateDocumentRevisionRequest shape (revision + optional if_document_updated_at)
// and the CLI docs revise envelope (if_base_revision + content + content_type), matching PATCH /docs/{document_id}.
func handleCreateDocumentRevision(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	documentID, ok = resolveHTTPResourceID(w, r, opts, "document", documentID, "document")
	if !ok {
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
		if _, hasRefs := rev["refs"]; !hasRefs {
			writeError(w, http.StatusBadRequest, "invalid_request", "revision.refs is required")
			return
		}
		refs, err := extractStringSlice(rev["refs"])
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
			"request_key":      probe["request_key"],
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
	var ok bool
	documentID, ok = resolveHTTPResourceID(w, r, opts, "document", documentID, "document")
	if !ok {
		return
	}

	var req struct {
		RequestKey     string         `json:"request_key"`
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
		if _, has := req.Document["labels"]; has {
			writeError(w, http.StatusBadRequest, "invalid_request", "document.labels is not supported")
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
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	var hygiene markdownHygieneCollector
	if req.Document != nil {
		if !hygiene.normalizeMapString(w, "document.summary", req.Document, "summary") {
			return
		}
	}
	if !normalizeAnyMarkdownContent(w, &hygiene, "content", req.ContentType, &req.Content) {
		return
	}

	_, _, err = opts.primitiveStore.GetDocument(r.Context(), documentID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document")
		return
	}

	replayCtx, finishPreload, ok := preloadReplayDocumentContent(w, r.Context(), opts.primitiveStore, req.RequestKey, req.Content, req.ContentType)
	if !ok {
		return
	}
	status, response, err := executeAtomicMutation(replayCtx, opts.primitiveStore, "docs.revise", actorID, req.RequestKey, struct {
		ID   string
		Body any
	}{documentID, req}, successStatus, func(ctx context.Context) (map[string]any, error) {
		document, revision, err := opts.primitiveStore.UpdateDocument(ctx, actorID, documentID, req.Document, req.IfBaseRevision, req.Content, req.ContentType, refs, req.Provenance)
		if err != nil {
			return nil, err
		}
		return hygiene.attach(map[string]any{"document": document, "revision": revision}), nil
	})
	finishPreload()
	if err != nil {
		switch {
		case writePrimitiveQuotaViolationError(w, err):
			return
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "document not found")
		case errors.Is(err, primitives.ErrInvalidDocumentRequest):
			writeInvalidDocumentRequest(w, err)
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "document has been updated; refresh and retry")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to update document")
		}
		return
	}
	notifyProjectionMaintainer(opts)

	writeJSON(w, status, response)
}

func handlePutDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
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
		Document    map[string]any `json:"document"`
		Content     any            `json:"content"`
		ContentType string         `json:"content_type"`
		Refs        any            `json:"refs"`
	}
	if !decodeJSONBody(w, r, &req) {
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
	if req.Document == nil {
		req.Document = map[string]any{}
	}
	handleKey := strings.TrimSpace(strings.TrimPrefix(documentID, "document:"))
	if handleKey == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "document handle is required")
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
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
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeMapString(w, "document.summary", req.Document, "summary") {
		return
	}
	if !normalizeAnyMarkdownContent(w, &hygiene, "content", req.ContentType, &req.Content) {
		return
	}

	resolved, resolveErr := opts.primitiveStore.ResolveResourceRef(r.Context(), primitives.ResourceRefInput{Type: "document", Ref: documentID})
	if resolveErr != nil && !errors.Is(resolveErr, primitives.ErrNotFound) && !errors.Is(resolveErr, primitives.ErrInvalidResourceRef) {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve document")
		return
	}
	if resolveErr == nil {
		existing, revision, err := opts.primitiveStore.GetDocument(r.Context(), resolved.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document")
			return
		}
		baseRevision := firstNonEmptyString(revision["revision_id"], existing["head_revision_id"])
		patch := map[string]any{}
		if title := strings.TrimSpace(anyString(req.Document["title"])); title != "" {
			patch["title"] = title
		}
		if _, has := req.Document["summary"]; has {
			patch["summary"] = req.Document["summary"]
		}
		if _, has := req.Document["source"]; has {
			patch["source"] = req.Document["source"]
		}
		if _, has := req.Document["tags"]; has {
			patch["tags"] = req.Document["tags"]
		}
		if _, has := req.Document["hosts"]; has {
			patch["hosts"] = req.Document["hosts"]
		}
		if _, has := req.Document["verified_at"]; has {
			patch["verified_at"] = req.Document["verified_at"]
		}
		document, nextRevision, err := opts.primitiveStore.UpdateDocument(r.Context(), actorID, resolved.ID, patch, baseRevision, req.Content, req.ContentType, refs, nil)
		if err != nil {
			if writePrimitiveQuotaViolationError(w, err) {
				return
			}
			if errors.Is(err, primitives.ErrConflict) {
				writeError(w, http.StatusConflict, "conflict", "document revision conflict")
				return
			}
			if errors.Is(err, primitives.ErrInvalidDocumentRequest) {
				writeInvalidDocumentRequest(w, err)
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to update document")
			return
		}
		notifyProjectionMaintainer(opts)
		writeJSON(w, http.StatusOK, hygiene.attach(map[string]any{"document": document, "revision": nextRevision}))
		return
	}

	if strings.TrimSpace(anyString(req.Document["title"])) == "" {
		req.Document["title"] = handleKey
	}
	req.Document["handle"] = handleKey
	document, revision, err := opts.primitiveStore.CreateDocument(r.Context(), actorID, req.Document, req.Content, req.ContentType, refs)
	if err != nil {
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
	writeJSON(w, http.StatusCreated, hygiene.attach(map[string]any{"document": document, "revision": revision}))
}
