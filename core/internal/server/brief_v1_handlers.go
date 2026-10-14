package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
)

type briefStore interface {
	Brief(ctx context.Context, reader primitives.BriefReader, now time.Time) (map[string]any, error)
	RefreshBriefInitiatives(ctx context.Context, limit int) (bool, error)
}

// briefFreshnessBatch folds a few queued cards before the read so a write is
// visible on the next brief without waiting for the maintenance tick.
const briefFreshnessBatch = 50

// handleGetBriefV1 serves brief.v1: the caller's decisions, initiatives
// against their promises, own asks and answers, for this workspace only.
func handleGetBriefV1(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(briefStore)
	if !ok {
		writeError(w, 503, "brief_unavailable", "brief store is not configured")
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
	_, _ = store.RefreshBriefInitiatives(r.Context(), briefFreshnessBatch)
	ctx := r.Context()
	if canonical, ok := opts.primitiveStore.(*primitives.Store); ok {
		readCtx, closeRead, err := canonical.BeginResourceRead(ctx)
		if err != nil {
			writeError(w, 500, "internal_error", "brief could not be loaded")
			return
		}
		defer closeRead()
		ctx = readCtx
	}
	brief, err := store.Brief(ctx, reader, time.Now().UTC())
	if err != nil {
		writeError(w, 500, "internal_error", "brief could not be loaded")
		return
	}
	writeJSON(w, http.StatusOK, brief)
}

func firstNonEmptyBriefString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
