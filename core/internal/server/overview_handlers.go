package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/resourceaccess"
)

type overviewStore interface {
	DashboardReports(context.Context) (map[string]any, error)
	Overview(context.Context, map[string]bool, map[string]bool) (map[string]any, error)
	SetWorkspaceDashboard(context.Context, string, string) error
	HiddenSubjectRefs(context.Context) (map[string]bool, error)
}

func handleListWorkspaceDashboardReports(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(overviewStore)
	if !ok {
		writeError(w, 503, "overview_unavailable", "overview store is not configured")
		return
	}
	var payload map[string]any
	var err error
	if paged, ok := store.(interface {
		DashboardReportsPage(context.Context, string) (map[string]any, error)
	}); ok {
		payload, err = paged.DashboardReportsPage(r.Context(), r.URL.Query().Get("cursor"))
	} else {
		payload, err = store.DashboardReports(r.Context())
	}
	if errors.Is(err, primitives.ErrInvalidCursor) {
		writeError(w, 400, "invalid_request", "invalid dashboard cursor")
		return
	}
	if err != nil {
		writeError(w, 500, "internal_error", "dashboard reports could not be loaded")
		return
	}
	writeJSON(w, 200, payload)
}

func handleSetWorkspaceDashboard(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(overviewStore)
	if !ok {
		writeError(w, 503, "overview_unavailable", "overview store is not configured")
		return
	}
	var raw map[string]any
	if !decodeJSONBody(w, r, &raw) {
		return
	}
	value, present := raw["document_ref"]
	if !present {
		writeError(w, 400, "invalid_request", "document_ref is required; null clears the pin")
		return
	}
	actor, ok := resolveWriteActorID(w, r, opts, anyString(raw["actor_id"]))
	if !ok {
		return
	}
	id := ""
	if value != nil {
		ref, valid := value.(string)
		if !valid || strings.TrimSpace(ref) == "" {
			writeError(w, 400, "invalid_request", "document_ref must be a document ref or null")
			return
		}
		id, ok = resolveHTTPResourceID(w, r, opts, "document", ref, "document")
		if !ok {
			return
		}
	}
	if err := store.SetWorkspaceDashboard(r.Context(), actor, id); err != nil {
		workStoreError(w, r, err)
		return
	}
	handleGetOverview(w, r, opts)
}

func handleGetOverview(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	view := r.URL.Query().Get("work_view")
	if view != "" && view != "full" && view != "summary" {
		writeError(w, 400, "invalid_request", "work_view must be full or summary")
		return
	}
	if view == "summary" {
		r = r.WithContext(primitives.WithOverviewWorkSummary(r.Context()))
	}
	store, ok := opts.primitiveStore.(overviewStore)
	if !ok {
		writeError(w, 503, "overview_unavailable", "overview store is not configured")
		return
	}
	humanIDs := map[string]bool{}
	agentNames := map[string]bool{}
	now := time.Now().UTC()
	var payload map[string]any
	var err error
	visitStore, scoped := opts.primitiveStore.(overviewVisitStore)
	stage := time.Now()
	func() {
		ctx := r.Context()
		if canonical, ok := opts.primitiveStore.(*primitives.Store); ok {
			var close func()
			ctx, close, err = canonical.BeginOverviewRead(ctx)
			if err != nil {
				return
			}
			defer close()
		}
		if scoped {
			payload, err = visitStore.OverviewVisible(ctx, humanIDs, agentNames, planVisibility(r, opts), now, planStalledAfter())
		} else {
			payload, err = store.Overview(ctx, humanIDs, agentNames)
		}
	}()
	addServerTiming(w, "projection", stage)
	if err != nil {
		writeError(w, 500, "internal_error", "overview could not be loaded")
		return
	}
	visitWork, _ := payload["_visit_work"].([]map[string]any)
	delete(payload, "_visit_work")
	if scoped {
		stage = time.Now()
		digest, e := visitStore.OverviewChanges(r.Context(), overviewPrincipal(r), visitWork, payload["work"].(map[string]any)["truncated"] == true, planVisibility(r, opts), now)
		if e == nil {
			e = appendOverviewDecisions(r, opts, &digest, now)
		}
		if e != nil {
			writeError(w, 500, "internal_error", "overview changes could not be loaded")
			return
		}
		payload["since_you_last_looked"] = digest
		addServerTiming(w, "changes", stage)
	}
	work := payload["work"].(map[string]any)
	items := work["items"].([]map[string]any)
	public := make([]map[string]any, 0, len(items))
	for _, item := range items {
		public = append(public, publicWork(item))
	}
	work["items"] = public
	needs := payload["needs_you"].(map[string]any)
	needs["truncated"] = work["truncated"] == true
	rows := needs["rows"].([]map[string]any)
	stage = time.Now()
	inbox, inboxPartial, err := loadOverviewInboxItems(r, opts)
	addServerTiming(w, "inbox", stage)
	needs["truncated"] = needs["truncated"] == true || inboxPartial
	if err != nil {
		needs["status"] = "unavailable"
		needs["message"] = "Needs you could not be loaded."
	} else {
		for _, item := range inbox {
			rows = append([]map[string]any{{"id": "inbox:" + anyString(item["id"]), "title": firstNonEmptyString(anyString(item["title"]), "Request"), "source": anyString(item["requester_label"]), "href": "/inbox?mailbox=needs-you&item=" + url.QueryEscape("inbox:"+anyString(item["id"]))}}, rows...)
		}
	}
	stage = time.Now()
	if opts.pmRuntime != nil && opts.pmRuntime.Service != nil {
		if principal, ok := cachedAuthenticatedPrincipal(r); ok {
			p := pm.Principal{WorkspaceID: opts.pmRuntime.cfg.PM.WorkspaceID, ActorID: principal.ActorID, Human: principal.PrincipalKind == "human"}
			decisions, actions, partial, e := opts.pmRuntime.Service.OverviewDecisions(r.Context(), p)
			needs["truncated"] = needs["truncated"] == true || partial
			hidden := map[string]bool{}
			if resolver, ok := opts.primitiveStore.(planStore); ok && e == nil {
				refs := []string{}
				_, canonical := opts.primitiveStore.(*primitives.Store)
				for _, d := range decisions {
					// Awaiting decisions already carry the scoped live snapshot
					// used by PM presentation. Only historical action context
					// needs the broader archived-but-readable ref projection.
					if !canonical || d.Status != pm.AwaitingAnswer {
						refs = append(refs, d.WorkRef)
					}
				}
				visible := planVisibility(r, opts)
				if _, canonical := opts.primitiveStore.(*primitives.Store); canonical {
					if _, scoped := resourceaccess.PolicyFrom(r.Context()); scoped {
						visible = nil
					}
				}
				previews, err := resolver.ResolveRefs(r.Context(), refs, visible, now, planStalledAfter())
				if err != nil {
					e = err
				} else {
					for i, preview := range previews {
						if !preview.Resolvable {
							hidden[refs[i]] = true
						}
					}
				}
			}
			if e != nil {
				needs["status"] = "unavailable"
				needs["message"] = "Decisions could not be loaded."
			} else {
				for _, d := range decisions {
					if hidden[d.WorkRef] || (d.Status == pm.AwaitingAnswer && (d.WorkMissing || (d.AlreadyAtTarget != nil && *d.AlreadyAtTarget) || (d.TargetCurrent != nil && !*d.TargetCurrent))) {
						continue
					}
					actionable := d.Status == pm.AwaitingAnswer && d.CanAnswer
					for _, a := range actions[d.ID] {
						if a.DecisionID != d.ID || d.Status != pm.Answered {
							continue
						}
						if a.Status == pm.Failed {
							actionable = true
						}
						if (a.Status == pm.Pending || a.Status == "pending") && a.Deliverable && d.ActorID == p.ActorID {
							sent := false
							for _, attempt := range a.Attempts {
								if attempt.SentAt != nil {
									sent = true
								}
							}
							if !sent {
								actionable = true
							}
						}
					}
					if actionable {
						rows = append(rows, map[string]any{"id": "decision:" + d.ID, "title": firstNonEmptyString(d.Instruction, "Decision"), "source": "Decision", "href": "/inbox?mailbox=needs-you&item=" + url.QueryEscape("decision:"+d.ID)})
					}
				}
			}
		}
	}
	addServerTiming(w, "decisions", stage)
	needs["rows"] = rows
	needs["count"] = len(rows)
	payload["agents"] = map[string]any{"status": "unavailable", "message": "Agents could not be loaded."}
	stage = time.Now()
	if opts.runStore != nil {
		func() {
			ctx := r.Context()
			if canonical, ok := opts.primitiveStore.(*primitives.Store); ok {
				next, close, e := canonical.BeginOverviewRead(ctx)
				if e != nil {
					return
				}
				ctx = next
				defer close()
			}
			roster, partial, e := opts.runStore.OverviewRoster(ctx, time.Now().UTC())
			if e == nil {
				payload["agents"] = map[string]any{"status": "ok", "items": roster, "truncated": partial}
			}
		}()
	}
	addServerTiming(w, "roster", stage)
	if scoped && r.URL.Path == "/overview" {
		stage = time.Now()
		if err = visitStore.RecordOverviewVisit(r.Context(), overviewPrincipal(r), visitWork, now); err != nil {
			writeError(w, 500, "internal_error", "overview visit could not be recorded")
			return
		}
		addServerTiming(w, "visit", stage)
	}
	w.Header().Set("Cache-Control", "no-store")
	if view == "summary" {
		for i, item := range public {
			public[i] = compactOverviewWork(item)
		}
	}
	writeJSON(w, 200, payload)
}
