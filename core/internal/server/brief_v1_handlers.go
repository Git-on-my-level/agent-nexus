package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

type briefStore interface {
	Brief(ctx context.Context, reader primitives.BriefReader, now time.Time) (map[string]any, error)
}

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
	if reader.Kind == "human" {
		appendBriefPMDecisions(r, opts, brief)
	}
	writeJSON(w, http.StatusOK, brief)
}

// appendBriefPMDecisions adds PM decision records awaiting this human's
// answer as pm_proposal decisions, through the PM service's bounded,
// permission-checked awaiting-decision read. A PM failure never fails the brief.
func appendBriefPMDecisions(r *http.Request, opts handlerOptions, brief map[string]any) {
	if opts.pmRuntime == nil || opts.pmRuntime.Service == nil {
		return
	}
	principal, ok := cachedAuthenticatedPrincipal(r)
	if !ok {
		return
	}
	p := pm.Principal{WorkspaceID: opts.pmRuntime.cfg.PM.WorkspaceID, ActorID: principal.ActorID, Human: true}
	decisions, partial, err := opts.pmRuntime.Service.AwaitingDecisions(r.Context(), p)
	if err != nil {
		return
	}
	items, _ := brief["decisions"].([]map[string]any)
	now := time.Now().UTC()
	for _, d := range decisions {
		if d.Status != pm.AwaitingAnswer || !d.CanAnswer {
			continue
		}
		title := strings.TrimSpace(d.Instruction)
		if runes := []rune(title); len(runes) > 160 {
			title = string(runes[:160])
		}
		items = append(items, map[string]any{
			"ref": "decision:" + d.ID, "inbox_item_id": d.ID, "kind": "pm_proposal", "title": firstNonEmptyBriefString(title, "PM proposal"),
			"requested_by": map[string]any{"id": firstNonEmptyBriefString(d.ProposedBy, d.ActorID), "display": firstNonEmptyBriefString(d.ProposedBy, d.ActorID)},
			"subject":      map[string]any{"ref": d.WorkRef}, "created_at": d.CreatedAt.UTC().Format(time.RFC3339), "age_seconds": int64(now.Sub(d.CreatedAt) / time.Second),
			"decide_by": nil, "overdue_for_seconds": nil, "blocks": []map[string]any{}, "blocks_count": 0,
			"proposals": []string{"Approve", "Reject"}, "pm_decision": map[string]any{"id": d.ID, "revision": d.Revision},
		})
	}
	primitives.SortBriefDecisions(items)
	truncated, _ := brief["truncated"].(map[string]bool)
	if len(items) > primitives.BriefSectionLimit {
		items = items[:primitives.BriefSectionLimit]
		if truncated != nil {
			truncated["decisions"] = true
		}
	}
	if partial && truncated != nil {
		truncated["decisions"] = true
	}
	brief["decisions"] = items
}

func firstNonEmptyBriefString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
