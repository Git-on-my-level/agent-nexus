package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

type overviewVisitStore interface {
	OverviewVisible(context.Context, map[string]bool, map[string]bool, func(string, string) bool, time.Time, time.Duration) (map[string]any, error)
	OverviewChanges(context.Context, string, []map[string]any, bool, func(string, string) bool, time.Time) (primitives.OverviewChanges, error)
	LoadOverviewChanges(context.Context, string, func(string, string) bool, time.Time, time.Duration) (primitives.OverviewChanges, error)
	RecordOverviewVisit(context.Context, string, []map[string]any, time.Time) error
}

func overviewPrincipal(r *http.Request) string {
	p, ok := cachedAuthenticatedPrincipal(r)
	if !ok || p == nil || p.AgentID == "" {
		return ""
	}
	return p.PrincipalKind + ":" + p.AgentID
}

func appendOverviewDecisions(r *http.Request, opts handlerOptions, d *primitives.OverviewChanges, now time.Time) error {
	if d.Since == nil || opts.pmRuntime == nil || opts.pmRuntime.Service == nil {
		return nil
	}
	p, ok := cachedAuthenticatedPrincipal(r)
	if !ok {
		return nil
	}
	since, err := time.Parse(time.RFC3339Nano, *d.Since)
	if err != nil {
		return err
	}
	decisions, partial, err := opts.pmRuntime.Service.NewDecisions(r.Context(), pm.Principal{WorkspaceID: opts.pmRuntime.cfg.PM.WorkspaceID, ActorID: p.ActorID, Human: p.PrincipalKind == "human"}, since, now)
	if errors.Is(err, pm.ErrNotOnboarded) {
		return nil
	}
	if err != nil {
		return err
	}
	d.Truncated = d.Truncated || partial
	refs := []string{}
	for _, decision := range decisions {
		refs = append(refs, decision.WorkRef)
	}
	store, ok := opts.primitiveStore.(planStore)
	if !ok {
		return nil
	}
	previews, err := store.ResolveRefs(r.Context(), refs, planVisibility(r, opts), now, planStalledAfter())
	if err != nil {
		return err
	}
	for i, decision := range decisions {
		if !previews[i].Resolvable {
			continue
		}
		d.Add(primitives.OverviewChange{Kind: "decision_created", Ref: "decision:" + decision.ID, Title: "New decision for " + previews[i].Title, TS: decision.CreatedAt.Format(time.RFC3339Nano)})
	}
	return nil
}

func handleOverviewChanges(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(overviewVisitStore)
	if !ok {
		writeError(w, 503, "overview_unavailable", "overview store is not configured")
		return
	}
	now := time.Now().UTC()
	d, err := store.LoadOverviewChanges(r.Context(), overviewPrincipal(r), planVisibility(r, opts), now, planStalledAfter())
	if err == nil {
		err = appendOverviewDecisions(r, opts, &d, now)
	}
	if err != nil {
		writeError(w, 500, "internal_error", "overview changes could not be loaded")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"since": d.Since, "generated_at": d.GeneratedAt, "items": d.Items, "truncated": d.Truncated})
}
