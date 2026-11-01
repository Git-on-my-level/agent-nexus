// Document comment handlers: list, create, update, and delete, plus comment
// mutation error mapping and optional refs parsing for comment payloads.

package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"agent-nexus-core/internal/primitives"
)

func handleListDocumentComments(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	documentID, ok = resolveHTTPResourceID(w, r, opts, "document", documentID, "document")
	if !ok {
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
	comments, nextCursor, err := opts.primitiveStore.ListDocumentComments(r.Context(), documentID, limitFilter, strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		if errors.Is(err, primitives.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list document comments")
		return
	}
	response := map[string]any{"comments": comments}
	if nextCursor != "" {
		response["next_cursor"] = nextCursor
	}
	writeJSON(w, http.StatusOK, response)
}

func handleCreateDocumentComment(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID, parentID string) {
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
		ActorID  string `json:"actor_id"`
		Text     string `json:"text"`
		ParentID string `json:"parent_id"`
		ReplyTo  string `json:"reply_to"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if parentID == "" {
		parentID = firstNonEmptyString(req.ReplyTo, req.ParentID)
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	comment, err := opts.primitiveStore.CreateDocumentComment(r.Context(), actorID, documentID, req.Text, parentID)
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		if errors.Is(err, primitives.ErrInvalidDocumentRequest) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create document comment")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"comment": comment})
}

func handleUpdateDocumentComment(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID, commentID string) {
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
		ActorID string `json:"actor_id"`
		Text    string `json:"text"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	comment, err := opts.primitiveStore.UpdateDocumentComment(r.Context(), actorID, documentID, commentID, req.Text)
	if err != nil {
		writeDocumentCommentMutationError(w, err, "failed to update document comment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comment": comment})
}

func handleDeleteDocumentComment(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID, commentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	documentID, ok = resolveHTTPResourceID(w, r, opts, "document", documentID, "document")
	if !ok {
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, "")
	if !ok {
		return
	}
	comment, err := opts.primitiveStore.DeleteDocumentComment(r.Context(), actorID, documentID, commentID)
	if err != nil {
		writeDocumentCommentMutationError(w, err, "failed to delete document comment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comment": comment})
}

func writeDocumentCommentMutationError(w http.ResponseWriter, err error, fallback string) {
	if writePrimitiveQuotaViolationError(w, err) {
		return
	}
	if errors.Is(err, primitives.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "comment not found")
		return
	}
	if errors.Is(err, primitives.ErrForbidden) {
		writeError(w, http.StatusForbidden, "forbidden", "only the comment author may change this comment")
		return
	}
	if errors.Is(err, primitives.ErrInvalidDocumentRequest) {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", fallback)
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
