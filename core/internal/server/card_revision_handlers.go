// Card revision reads: metadata-only history pages, one revision with its
// content, and a bounded diff between two revisions.

package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"agent-nexus-core/internal/primitives"
)

// handleCardRevisionRead serves GET /cards/{card_id}/revisions/{revision_id}
// and its /diff suffix.
func handleCardRevisionRead(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID, revisionPath string) {
	revisionID, diff := strings.CutSuffix(revisionPath, "/diff")
	if strings.TrimSpace(revisionID) == "" {
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	if diff {
		handleDiffCardRevisions(w, r, opts, cardID, revisionID)
		return
	}
	handleGetCardRevision(w, r, opts, cardID, revisionID)
}

func cardRevisionStoreError(w http.ResponseWriter, r *http.Request, err error, what string) {
	switch {
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", what+" not found")
	case errors.Is(err, primitives.ErrInvalidBoardRequest):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeInternalError(w, r, err, "failed to load "+what)
	}
}

// handleListCardRevisions serves one metadata-only page of card history,
// newest first. Revision content is never read here.
func handleListCardRevisions(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	cardID, ok = resolveHTTPResourceID(w, r, opts, "card", cardID, "card")
	if !ok {
		return
	}
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > primitives.MaxCardRevisionPageSize {
			writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("limit must be an integer between 1 and %d", primitives.MaxCardRevisionPageSize))
			return
		}
		limit = n
	}
	cardRef, cardHandle := resolvedPublicIdentity(r.Context(), opts, "card", cardID)
	page, err := opts.primitiveStore.ListCardRevisions(r.Context(), cardID, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		cardRevisionStoreError(w, r, err, "card revisions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"card_ref": cardRef, "card_handle": cardHandle, "card_id": cardID, "revisions": publicCardRevisionsView(page.Revisions), "next_cursor": page.NextCursor})
}

func resolveCardRevisionID(r *http.Request, opts handlerOptions, revisionID string) string {
	revisionID = strings.TrimSpace(revisionID)
	if resolved, err := opts.primitiveStore.ResolveResourceRef(r.Context(), primitives.ResourceRefInput{Type: "card_revision", Ref: revisionID}); err == nil {
		return resolved.ID
	}
	return strings.TrimPrefix(revisionID, "card_revision:")
}

// handleGetCardRevision returns one revision with its full content.
func handleGetCardRevision(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID, revisionID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	cardID, ok = resolveHTTPResourceID(w, r, opts, "card", cardID, "card")
	if !ok {
		return
	}
	cardRef, cardHandle := resolvedPublicIdentity(r.Context(), opts, "card", cardID)
	revision, err := opts.primitiveStore.GetCardRevision(r.Context(), cardID, resolveCardRevisionID(r, opts, revisionID))
	if err != nil {
		cardRevisionStoreError(w, r, err, "card revision")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"card_ref": cardRef, "card_handle": cardHandle, "card_id": cardID, "revision": publicCardRevisionView(revision)})
}

// handleDiffCardRevisions compares a revision with ?base= (default: its
// previous revision).
func handleDiffCardRevisions(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID, revisionID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	cardID, ok = resolveHTTPResourceID(w, r, opts, "card", cardID, "card")
	if !ok {
		return
	}
	base := strings.TrimSpace(r.URL.Query().Get("base"))
	if base != "" {
		base = resolveCardRevisionID(r, opts, base)
	}
	cardRef, cardHandle := resolvedPublicIdentity(r.Context(), opts, "card", cardID)
	diff, err := opts.primitiveStore.DiffCardRevisions(r.Context(), cardID, resolveCardRevisionID(r, opts, revisionID), base)
	if err != nil {
		cardRevisionStoreError(w, r, err, "card revision")
		return
	}
	if rev, ok := diff["revision"].(map[string]any); ok {
		diff["revision"] = publicCardRevisionView(rev)
	}
	if rev, ok := diff["base_revision"].(map[string]any); ok {
		diff["base_revision"] = publicCardRevisionView(rev)
	}
	diff["card_ref"], diff["card_handle"], diff["card_id"] = cardRef, cardHandle, cardID
	writeJSON(w, http.StatusOK, diff)
}

func publicCardRevisionsView(revisions []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(revisions))
	for _, revision := range revisions {
		out = append(out, publicCardRevisionView(revision))
	}
	return out
}

func publicCardRevisionView(revision map[string]any) map[string]any {
	out := copyStringAnyMap(revision)
	for _, key := range []string{"id", "card_id", "board_id", "thread_id"} {
		delete(out, key)
	}
	return out
}
