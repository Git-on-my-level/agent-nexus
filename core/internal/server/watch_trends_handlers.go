package server

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
)

func handleGetWatchTrends(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	days := 30
	switch strings.TrimSpace(r.URL.Query().Get("range")) {
	case "", "30d":
	case "7d":
		days = 7
	case "90d":
		days = 90
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "range must be 7d, 30d or 90d")
		return
	}
	group := strings.TrimSpace(r.URL.Query().Get("group"))
	if !slices.Contains(primitives.WatchTrendGroups, group) {
		writeError(w, http.StatusBadRequest, "invalid_request", "group must be decisions, initiatives or loops")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// Missing middleware must never confer an internal, unscoped read.
	if _, scoped := primitives.StreamReaderScope(r.Context()); !scoped {
		writeError(w, http.StatusForbidden, "restricted_trends", "Trends are unavailable for access-scoped readers.")
		return
	}
	store, ok := opts.primitiveStore.(interface {
		WatchTrends(context.Context, primitives.BriefReader, string, int, time.Time) (map[string]any, error)
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Trends are unavailable.")
		return
	}
	body, err := store.WatchTrends(r.Context(), primitives.BriefReader{}, group, days, time.Now().UTC())
	if errors.Is(err, primitives.ErrForbidden) {
		writeError(w, http.StatusForbidden, "restricted_trends", "Trends are unavailable for access-scoped readers.")
		return
	}
	if err != nil {
		writeReadInternalError(w, r, err, "failed to read trends")
		return
	}
	writeJSON(w, http.StatusOK, body)
}
