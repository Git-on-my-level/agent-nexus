// Document search: GET /documents/search and the optional boolean query
// parsing it introduced.

package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"agent-nexus-core/internal/primitives"
)

func parseOptionalBoolQuery(w http.ResponseWriter, raw, name string) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, true
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", name+" must be a boolean")
		return false, false
	}
	return parsed, true
}

func handleSearchDocuments(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	query := r.URL.Query()
	q := strings.TrimSpace(query.Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "q is required")
		return
	}
	var limitFilter *int
	limitRaw := strings.TrimSpace(query.Get("limit"))
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 1000")
			return
		}
		limitFilter = &parsed
	}
	knowledge, ok := parseOptionalBoolQuery(w, query.Get("knowledge"), "knowledge")
	if !ok {
		return
	}
	states, parseErr := ParseListLifecycleStates(query)
	if parseErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", parseErr.Error())
		return
	}
	coverage := primitives.DocumentSearchCoverage{}
	documents, nextCursor, err := opts.primitiveStore.SearchDocuments(r.Context(), primitives.DocumentSearchFilter{
		Coverage:  &coverage,
		States:    states,
		Query:     q,
		Tag:       strings.TrimSpace(query.Get("tag")),
		Host:      strings.TrimSpace(query.Get("host")),
		Knowledge: knowledge,
		Limit:     limitFilter,
		Cursor:    strings.TrimSpace(query.Get("cursor")),
	})
	if err != nil {
		if errors.Is(err, primitives.ErrSearchRepairPending) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "search_repair_pending", "aggregate search is being refreshed; retry")
			return
		}
		if errors.Is(err, primitives.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		if errors.Is(err, primitives.ErrInvalidDocumentRequest) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to search documents")
		return
	}
	response := map[string]any{"documents": documents, "search_coverage": coverage}
	if nextCursor != "" {
		response["next_cursor"] = nextCursor
	}
	writeJSON(w, http.StatusOK, response)
}
