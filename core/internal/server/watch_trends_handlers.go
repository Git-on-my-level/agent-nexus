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

// watchTrendsTTL is the response window. Every request inside one window is
// computed as of the window's end and cached by it, so generated_at and the
// bucket boundaries cannot move inside a window. The key carries the reader's
// own scope (actor, PM actor, admin), so a change to its own permissions takes
// effect at once, but never the workspace-wide authorization epoch: that epoch
// moves on writes the reader cannot see, and a recomputation it forced would
// tell the reader those writes happened. Visible changes appear within one
// window. Trends move over days.
const watchTrendsTTL = time.Minute

// watchTrendsWindow is the start of the response window containing now.
func watchTrendsWindow(now time.Time) time.Time { return now.UTC().Truncate(watchTrendsTTL) }

// watchTrendsAsOf is the instant a window's response describes: its end, so a
// record written earlier in the window is counted.
func watchTrendsAsOf(now time.Time) time.Time { return watchTrendsWindow(now).Add(watchTrendsTTL) }

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
	}
	now := watchTrendsAsOf(time.Now())
	if scope, ok := primitives.StreamReaderScope(ctx); ok {
		raw, _ := json.Marshal([]any{"watch-trends", scope, reader.ActorID, group, days, now.Unix()})
		key = string(raw)
	}
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
