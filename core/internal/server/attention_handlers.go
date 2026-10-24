package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
	reports "agent-nexus-visualreport"
)

func attentionStore(w http.ResponseWriter, r *http.Request, opts handlerOptions) (*primitives.Store, bool) {
	if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
		return nil, false
	}
	store, ok := opts.primitiveStore.(*primitives.Store)
	if !ok {
		writeError(w, 503, "unavailable", "attention store unavailable")
		return nil, false
	}
	return store, true
}
func attentionRead(w http.ResponseWriter, r *http.Request, store *primitives.Store) (*http.Request, func(), bool) {
	ctx, closeRead, err := store.BeginResourceRead(r.Context())
	if err != nil {
		workStoreError(w, r, err)
		return r, func() {}, false
	}
	return r.WithContext(ctx), closeRead, true
}
func handleAttentionAgent(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := attentionStore(w, r, opts)
	if !ok {
		return
	}
	if r.Method == http.MethodPut {
		p, ok := requireHumanPrincipal(w, r, opts)
		if !ok {
			return
		}
		var req map[string]any
		if !decodeJSONBody(w, r, &req) {
			return
		}
		value, present := req["attention_agent"]
		if !present || len(req) != 1 {
			writeError(w, 400, "invalid_request", "attention_agent is required; null clears it")
			return
		}
		var ref *string
		if value != nil {
			v, ok := value.(string)
			if !ok || v == "" {
				writeError(w, 400, "invalid_request", "attention_agent must be a principal ref or null")
				return
			}
			ref = &v
		}
		if err := store.SetAttentionAgent(r.Context(), p.ActorID, ref); err != nil {
			workStoreError(w, r, err)
			return
		}
	}
	ref, err := store.AttentionAgent(r.Context())
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"attention_agent": ref})
}
func handleAttentionPlace(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := attentionStore(w, r, opts)
	if !ok {
		return
	}
	p, _ := cachedAuthenticatedPrincipal(r)
	var req primitives.AttentionPlacement
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if len(req.RequestKey) > 200 {
		writeError(w, 400, "invalid_request", "request_key exceeds 200 bytes")
		return
	}
	status, response, err := executeAtomicMutation(r.Context(), store, "attention.place", p.ActorID, req.RequestKey, req, 201, func(ctx context.Context) (map[string]any, error) {
		// Persist the HTTP result in the outer replay transaction. The inner
		// placement needs no second receipt, and its savepoint shares that commit.
		placement := req
		placement.RequestKey = ""
		event, err := store.PlaceAttention(ctx, p.ActorID, placement)
		if err != nil {
			return nil, err
		}
		payload, _ := event["payload"].(map[string]any)
		title := req.SubjectRef
		subject := anyString(payload["subject_ref"])
		if facts, err := store.ResolveRefs(ctx, []string{subject}, nil, time.Now()); err == nil && len(facts) > 0 && facts[0].Resolvable {
			subject = facts[0].Ref
			title = facts[0].Title
		}
		return map[string]any{"subject_ref": subject, "title": title, "surface": payload["surface"], "reason": payload["reason"], "placed_by": payload["placed_by"], "rank": payload["rank"], "placed_at": event["ts"], "changed_since_last_visit": false}, nil
	})
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	writeJSON(w, status, response)
}
func handleAttentionItems(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := attentionStore(w, r, opts)
	if !ok {
		return
	}
	p, _ := cachedAuthenticatedPrincipal(r)
	actor, kind := p.ActorID, p.PrincipalKind
	if ref := r.URL.Query().Get("for"); ref != "" && ref != "me" && ref != "actor:"+actor && ref != p.PrincipalKind+":"+p.AgentID {
		if !isAuthAdminPrincipal(p) {
			writeError(w, 403, "forbidden", "agents may read only their own attention items; humans and auth-admins may select another principal")
			return
		}
		var err error
		actor, kind, err = store.AttentionActor(r.Context(), ref)
		if err != nil {
			workStoreError(w, r, err)
			return
		}
	}
	surface := r.URL.Query().Get("surface")
	if surface == "" {
		surface = "all"
	}
	if surface != "all" && !primitives.AttentionSurface(surface) {
		writeError(w, 400, "invalid_request", "unknown attention surface")
		return
	}
	since := r.URL.Query().Get("since")
	if since != "" {
		at, err := time.Parse(time.RFC3339Nano, since)
		if err != nil {
			if d, e := time.ParseDuration(since); e != nil || d <= 0 {
				writeError(w, 400, "invalid_request", "since must be RFC3339 or a positive duration")
				return
			}
		} else {
			since = at.UTC().Format("2006-01-02T15:04:05.000000000Z")
		}
	}
	scope := r.URL.Query().Get("scope_ref")
	if scope == "" {
		scope = "builtin:initiatives"
	}
	var err error
	scope, err = attentionScope(r, store, scope)
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	r, closeRead, ok := attentionRead(w, r, store)
	if !ok {
		return
	}
	defer closeRead()
	page, err := store.AttentionItems(r.Context(), actor, kind, surface, since, scope, r.URL.Query().Get("cursor"), time.Now().UTC())
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": page.Items, "next_cursor": page.NextCursor})
}
func handleAttentionFeedback(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := attentionStore(w, r, opts)
	if !ok {
		return
	}
	p, _ := cachedAuthenticatedPrincipal(r)
	if r.Method == http.MethodPost {
		if _, ok := requireHumanPrincipal(w, r, opts); !ok {
			return
		}
		var req primitives.AttentionFeedback
		if !decodeJSONBody(w, r, &req) {
			return
		}
		event, err := store.RecordAttentionFeedback(r.Context(), p.ActorID, req)
		if err != nil {
			workStoreError(w, r, err)
			return
		}
		writeJSON(w, 201, event)
		return
	}
	// Any human may read feedback, defaulting to their own; workspace agents
	// read all workspace feedback unless they filter by recipient.
	recipient := ""
	ref := r.URL.Query().Get("for")
	if ref == "" && p.PrincipalKind != "agent" {
		ref = "me"
	}
	if ref != "" {
		if ref == "me" {
			recipient = p.ActorID
		} else {
			var err error
			recipient, _, err = store.AttentionActor(r.Context(), ref)
			if err != nil {
				workStoreError(w, r, err)
				return
			}
		}
	}
	if p.PrincipalKind != "agent" && !isAuthAdminPrincipal(p) && recipient != p.ActorID {
		writeError(w, 403, "forbidden", "attention feedback is readable by humans, auth-admins and workspace agents")
		return
	}
	since := r.URL.Query().Get("since")
	if since != "" {
		at, err := time.Parse(time.RFC3339Nano, since)
		if err != nil {
			if d, e := time.ParseDuration(since); e != nil || d <= 0 {
				writeError(w, 400, "invalid_request", "since must be RFC3339 or a positive duration")
				return
			}
		} else {
			since = at.UTC().Format("2006-01-02T15:04:05.000000000Z")
		}
	}
	r, closeRead, ok := attentionRead(w, r, store)
	if !ok {
		return
	}
	defer closeRead()
	page, err := store.AttentionFeedbackList(r.Context(), recipient, since, r.URL.Query().Get("cursor"))
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, page)
}
func attentionScope(r *http.Request, store *primitives.Store, scope string) (string, error) {
	if scope == "builtin:initiatives" {
		return scope, nil
	}
	if !strings.HasPrefix(scope, "document:") && !strings.HasPrefix(scope, "doc:") {
		return "", primitives.ErrInvalidResourceRef
	}
	resolved, err := store.ResolveResourceRef(r.Context(), primitives.ResourceRefInput{Ref: scope})
	if err != nil {
		return "", err
	}
	return "document:" + resolved.ID, nil
}
func attentionDashboardSnapshot(r *http.Request, opts handlerOptions, store *primitives.Store, scope string, now time.Time) (primitives.AttentionSnapshot, bool, error) {
	if scope == "builtin:initiatives" {
		return store.AttentionWorkSnapshot(r.Context(), now)
	}
	_, revision, err := store.GetDocument(r.Context(), strings.TrimPrefix(scope, "document:"))
	if err != nil {
		return nil, false, err
	}
	panels, err := reports.ParseAll(revision["content"])
	if err != nil {
		return nil, false, primitives.ErrInvalidWorkRequest
	}
	_, results := materializeReportPanels(r, opts, panels)
	snapshot := primitives.AttentionSnapshot{}
	truncated := false
	// Reuse report queries and their visibility rules; snapshots contain only the
	// rendered subjects. Fixed caps prevent large reports expanding the ledger.
	var visit func(any)
	visit = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				visit(item)
			}
		case map[string]any:
			ref := anyString(x["ref"])
			if strings.HasPrefix(ref, "card:") {
				if _, ok := snapshot[ref]; !ok && len(snapshot) == 100 {
					truncated = true
					return
				}
				v := snapshot[ref]
				state := mapValueAttention(v.State)
				if state == nil {
					state = map[string]any{}
				}
				for _, key := range []string{"state", "phase"} {
					if value, present := x[key]; present {
						state[key] = value
					}
				}
				plan := x["plan_state"]
				if plan == nil {
					plan = x["progress"]
				}
				if compact := compactAttentionPlan(plan); compact != nil {
					v.Plan = compact
				}
				if goal := mapValueAttention(x["goal"]); goal != nil {
					v.Metric = goal["latest"]
				}
				// Existing loop reports nest health and goal under loop; collection
				// rows carry state and a spark progress series directly.
				if loop := mapValueAttention(x["loop"]); loop != nil {
					state["health"] = loop["health"]
					v.Metric = mapValueAttention(loop["goal"])["latest"]
				}
				if points, ok := mapValueAttention(x["progress"])["points"].([]any); ok {
					v.Metric = nil
					if len(points) > 0 {
						v.Metric = mapValueAttention(points[len(points)-1])["v"]
					}
				}
				v.Title, v.State, v.UpdatedAt = anyString(x["title"]), state, anyString(x["updated_at"])
				snapshot[ref] = v
				return
			}
			for _, value := range x {
				visit(value)
			}
		}
	}
	for _, result := range results {
		if result["status"] != "ok" {
			return nil, false, errors.New("dashboard panel unavailable")
		}
		if result["truncated"] == true {
			truncated = true
		}
		raw, err := json.Marshal(result["data"])
		if err != nil {
			return nil, false, err
		}
		var data any
		if err = json.Unmarshal(raw, &data); err != nil {
			return nil, false, err
		}
		visit(data)
	}
	return snapshot, truncated, nil
}
func mapValueAttention(v any) map[string]any { m, _ := v.(map[string]any); return m }

// Baselines retain status identities, not historical titles or linked refs.
// Removing a now-private step must never disclose its old content in a delta.
func compactAttentionPlan(value any) any {
	plan := mapValueAttention(value)
	steps, ok := plan["steps"].([]any)
	if !ok {
		steps, ok = plan["segments"].([]any)
	}
	if !ok {
		return nil
	}
	out := []map[string]any{}
	for _, value := range steps {
		step := mapValueAttention(value)
		out = append(out, map[string]any{"id": step["id"], "status": step["status"]})
	}
	return out
}
func handleAttentionVisit(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := attentionStore(w, r, opts)
	if !ok {
		return
	}
	p, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	var req map[string]any
	if !decodeJSONBody(w, r, &req) {
		return
	}
	scopeRef, valid := req["scope_ref"].(string)
	if !valid || len(req) != 1 {
		writeError(w, 400, "invalid_request", "only scope_ref is accepted; the authenticated principal owns the visit")
		return
	}
	scope, err := attentionScope(r, store, scopeRef)
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	now := time.Now().UTC()
	read, closeRead, ok := attentionRead(w, r, store)
	if !ok {
		return
	}
	snapshot, _, err := attentionDashboardSnapshot(read, opts, store, scope, now)
	closeRead()
	if err == nil {
		err = store.RecordAttentionVisit(r.Context(), "actor:"+p.ActorID, scope, snapshot, now)
	}
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"scope_ref": scopeRef, "visited_at": now.Format(time.RFC3339Nano)})
}
func handleAttentionChanges(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := attentionStore(w, r, opts)
	if !ok {
		return
	}
	p, _ := cachedAuthenticatedPrincipal(r)
	scope, err := attentionScope(r, store, r.URL.Query().Get("scope_ref"))
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	baseline, err := store.AttentionVisitChanges(r.Context(), "actor:"+p.ActorID, scope, primitives.AttentionSnapshot{}, false)
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	if baseline.Since == nil {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"since": nil, "items": []primitives.AttentionChange{}, "changed_refs": []string{}, "truncated": false})
		return
	}
	r, closeRead, ok := attentionRead(w, r, store)
	if !ok {
		return
	}
	defer closeRead()
	snapshot, truncated, err := attentionDashboardSnapshot(r, opts, store, scope, time.Now().UTC())
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	changes, err := store.AttentionVisitChanges(r.Context(), "actor:"+p.ActorID, scope, snapshot, truncated)
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"since": changes.Since, "items": changes.Items, "changed_refs": changes.ChangedRefs, "truncated": changes.Truncated})
}
