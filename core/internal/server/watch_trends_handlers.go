package server

import (
	"net/http"
	"slices"
	"strings"

	"agent-nexus-core/internal/primitives"
)

// Canonical projections incorporate contributors with different visibility.
// No HTTP principal currently has an unrestricted resource capability. Until
// per-scope projections exist, reject independently of workspace contents,
// owner counts, authorization epochs or cached aggregates. Keep the guard at
// this boundary even if middleware ever omits the request's access scope.
func handleGetWatchTrends(w http.ResponseWriter, r *http.Request, _ handlerOptions) {
	switch strings.TrimSpace(r.URL.Query().Get("range")) {
	case "", "7d", "30d", "90d":
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "range must be 7d, 30d or 90d")
		return
	}
	if !slices.Contains(primitives.WatchTrendGroups, strings.TrimSpace(r.URL.Query().Get("group"))) {
		writeError(w, http.StatusBadRequest, "invalid_request", "group must be decisions, initiatives or loops")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeError(w, http.StatusForbidden, "restricted_trends", "Trends are unavailable for access-scoped readers.")
}
