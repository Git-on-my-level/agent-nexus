package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
)

// watchTrendsTTL bounds how stale a trend may be. Trends move over days; a
// permission change still invalidates at once, because the key carries the
// authorization epoch and the reader's scope.
const watchTrendsTTL = time.Minute

type watchTrendsStore interface {
	WatchTrends(ctx context.Context, reader primitives.BriefReader, group string, days int, now time.Time) (map[string]any, error)
}

// handleGetWatchTrends serves one group of watch_trends.v1: the reader's view
// of whether the workspace is getting better or worse over 7, 30 or 90 days.
// Groups are separate requests so each stays within the ordinary read budget.
func handleGetWatchTrends(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(watchTrendsStore)
	if !ok {
		writeError(w, 503, "unavailable", "trends are unavailable")
		return
	}
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
	reader := primitives.BriefReader{Kind: "human", Visible: planVisibility(r, opts)}
	if p, ok := cachedAuthenticatedPrincipal(r); ok && p != nil {
		reader.ActorID = strings.TrimPrefix(p.ActorID, "actor:")
		reader.Display = firstNonEmptyBriefString(p.Username, p.AgentID, p.ActorID)
		if p.PrincipalKind != "" {
			reader.Kind = p.PrincipalKind
		}
	}
	ctx := r.Context()
	key := ""
	if canonical, ok := opts.primitiveStore.(*primitives.Store); ok {
		readCtx, closeRead, err := canonical.BeginResourceRead(ctx)
		if err != nil {
			writeError(w, 503, "unavailable", "trends are unavailable")
			return
		}
		defer closeRead()
		ctx = readCtx
		if token, err := canonical.ReportRenderToken(ctx); err == nil && token != "" {
			raw, _ := json.Marshal([]any{"watch-trends", token, reader.ActorID, group, days})
			key = string(raw)
		}
	}
	now := time.Now().UTC()
	if key != "" {
		if cached := opts.watchTrends.get(key, now); cached != nil {
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, cached)
			return
		}
	}
	trends, err := store.WatchTrends(ctx, reader, group, days, now)
	if err != nil {
		writeError(w, 503, "unavailable", "trends could not be computed")
		return
	}
	if key != "" {
		opts.watchTrends.put(key, trends, now)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, trends)
}
