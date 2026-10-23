package server

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	reports "agent-nexus-visualreport"
)

var collectionStates = map[string][][3]string{
	"initiatives": {{"waiting_on_you", "Waiting on you", "attention"}, {"at_risk", "At risk", "warning"}, {"stalled", "Stalled", "quiet"}, {"on_track", "On track", "good"}},
	"loops":       {{"failing", "Failing", "warning"}, {"stopped_reporting", "Stopped reporting", "warning"}, {"flat", "Flat", "quiet"}, {"improving", "Improving", "good"}, {"unknown", "Unknown", "neutral"}},
	"agents":      {{"stuck", "Stuck", "attention"}, {"offline", "Offline", "quiet"}, {"idle", "Idle", "neutral"}, {"working", "Working", "good"}},
}

func reportMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func stringPointer(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func collectionBase(ref, title, updated string) map[string]any {
	return map[string]any{"ref": ref, "title": title, "subtitle": nil, "owner": nil, "state": "unknown", "progress": map[string]any{"kind": "label", "label": ""}, "time": nil, "activity": make([]int, 14), "next_actor": nil, "updated_at": updated}
}
func (reader *reportReader) collection(q reports.Query) (map[string]any, bool, error) {
	store, ok := reader.opts.primitiveStore.(*primitives.Store)
	if !ok {
		return nil, false, fmt.Errorf("collection store unavailable")
	}
	rows := []map[string]any{}
	partial := false
	project := q.ProjectRef
	if project != "" {
		resolved, err := store.ResolveResourceRef(reader.r.Context(), primitives.ResourceRefInput{Type: "topic", Ref: project})
		if err != nil {
			return nil, false, err
		}
		topic, err := store.GetTopic(reader.r.Context(), resolved.ID)
		if err != nil {
			return nil, false, err
		}
		project = anyString(topic["ref"])
		if !reportActive(topic) {
			return nil, false, fmt.Errorf("project unavailable")
		}
	}
	principal, _ := cachedAuthenticatedPrincipal(reader.r)
	viewer := ""
	human := false
	if principal != nil {
		viewer = principal.ActorID
		human = principal.PrincipalKind == "human"
	}
	if q.Collection == "agents" {
		if reader.opts.runStore == nil {
			return nil, false, fmt.Errorf("roster unavailable")
		}
		roster, more, err := reader.opts.runStore.CollectionRoster(reader.r.Context(), reader.now)
		if err != nil {
			return nil, false, err
		}
		partial = more
		cardRefs := []string{}
		actorIDs := []string{}
		for _, a := range roster {
			if a.CurrentCardRef != nil {
				cardRefs = append(cardRefs, *a.CurrentCardRef)
			}
			actorIDs = append(actorIDs, a.ActorID)
		}
		works, err := store.ReportWorkSnapshots(reader.r.Context(), cardRefs)
		if err != nil {
			return nil, false, err
		}
		counts, err := store.CollectionActivity(reader.r.Context(), actorIDs, true, reader.now)
		if err != nil {
			return nil, false, err
		}
		for _, a := range roster {
			if a.RevokedAt != nil {
				continue
			}
			current := works[stringPointer(a.CurrentCardRef)]
			if project != "" && anyString(current["project_ref"]) != project {
				continue
			}
			row := collectionBase(a.Ref, firstNonEmptyString(a.DisplayName, a.Name, a.Handle), firstNonEmptyString(stringPointer(a.LastSignalAt), reader.now.Format(time.RFC3339Nano)))
			row["_project_ref"] = anyString(current["project_ref"])
			row["owner"] = map[string]any{"ref": a.Ref, "label": row["title"]}
			state := map[string]string{"waiting_on_human": "stuck", "stale": "offline", "idle": "idle", "working": "working"}[a.State]
			if state == "" {
				state = "offline"
			}
			row["state"] = state
			task := anyString(current["title"])
			if task == "" && state == "stuck" {
				task = "Waiting on a human"
			}
			row["subtitle"] = nullableText(task)
			row["progress"] = map[string]any{"kind": "label", "label": task}
			row["time"] = map[string]any{"label": "Last seen", "at": nullableText(stringPointer(a.LastSignalAt)), "tone": "neutral"}
			if days := counts[a.ActorID]; days != nil {
				row["activity"] = days
			}
			rows = append(rows, row)
		}
	} else {
		page, err := store.CollectionWork(reader.r.Context(), q.Collection, project, reader.now)
		if err != nil {
			return nil, false, err
		}
		partial = page.Truncated
		if q.Collection == "initiatives" {
			if err = store.EnrichCardPlans(reader.r.Context(), page.Work, planVisibility(reader.r, reader.opts), reader.now); err != nil {
				return nil, false, err
			}
		}
		refs := []string{}
		threads := []string{}
		for _, w := range page.Work {
			refs = append(refs, anyString(w["ref"]), "card:"+anyString(w["id"]))
			threads = append(threads, firstNonEmptyString(anyString(w["thread_id"]), anyString(w["parent_thread_id"])))
		}
		activity, err := store.CollectionActivity(reader.r.Context(), threads, false, reader.now)
		if err != nil {
			return nil, false, err
		}
		waiting := map[string]bool{}
		if q.Collection == "initiatives" && viewer != "" {
			waiting, err = store.WaitingCollectionRefs(reader.r.Context(), refs, viewer, human)
			if err != nil {
				return nil, false, err
			}
		}
		for _, w := range page.Work {
			row := collectionBase(anyString(w["ref"]), anyString(w["title"]), anyString(w["updated_at"]))
			row["_project_ref"] = anyString(w["project_ref"])
			if owner := anyString(w["owner"]); owner != "" {
				row["owner"] = map[string]any{"ref": owner, "label": owner}
			}
			if days := activity[firstNonEmptyString(anyString(w["thread_id"]), anyString(w["parent_thread_id"]))]; days != nil {
				row["activity"] = days
			}
			if q.Collection == "loops" {
				reader.collectionLoop(row, reportMap(w["loop"]))
				rows = append(rows, row)
				continue
			}
			next := anyString(w["next_actor"])
			nextRef := next
			kind := "agent"
			if next == "human" || (next == "actor:"+viewer && human) || (next == viewer && human) {
				kind = "human"
			}
			isViewer := next != "" && (next == viewer || next == "actor:"+viewer || next == "human" && human)
			if next == "human" || next == "agent" {
				nextRef = ""
			}
			if next != "" {
				row["next_actor"] = map[string]any{"kind": kind, "ref": nullableText(nextRef), "label": strings.ReplaceAll(next, "_", " "), "is_viewer": isViewer}
			}
			health, _ := w["plan_health"].(plans.Health)
			state := "on_track"
			switch {
			case isViewer || waiting[anyString(w["ref"])] || waiting["card:"+anyString(w["id"])]:
				state = "waiting_on_you"
			case health.State == "past_promise" || anyString(w["risk"]) == "high":
				state = "at_risk"
			case health.State == "no_promise" || anyString(w["phase"]) == "blocked":
				state = "stalled"
			}
			row["state"] = state
			if state == "waiting_on_you" && human {
				row["next_actor"] = map[string]any{"kind": "human", "ref": "actor:" + viewer, "label": viewer, "is_viewer": true}
			}
			row["subtitle"] = nullableText(anyString(w["next_action"]))
			if step, ok := w["next_step"].(*plans.NextStep); ok && step != nil {
				row["subtitle"] = nullableText(step.Title)
			}
			if plan, ok := w["plan"].(plans.Plan); ok {
				state, _ := w["plan_state"].(plans.State)
				status := map[string]string{}
				for _, step := range state.Steps {
					status[step.ID] = step.Status
				}
				segments := []map[string]any{}
				for _, step := range plan.Steps {
					value := map[string]string{"done": "done", "active": "current", "blocked": "blocked", "not_started": "todo"}[status[step.ID]]
					if value == "" {
						value = "todo"
					}
					segments = append(segments, map[string]any{"label": step.Title, "status": value})
				}
				row["progress"] = map[string]any{"kind": "segments", "segments": segments, "label": fmt.Sprintf("%d of %d steps", state.Progress.Done, state.Progress.Total)}
			} else {
				row["progress"] = map[string]any{"kind": "label", "label": "No plan"}
			}
			if health.Promise != nil {
				tone := "neutral"
				if health.State == "past_promise" {
					tone = "warning"
				}
				row["time"] = map[string]any{"label": health.Promise.What, "at": health.Promise.At, "tone": tone}
			}
			rows = append(rows, row)
		}
	}
	actorItems := []map[string]any{}
	for _, row := range rows {
		for _, key := range []string{"owner", "next_actor"} {
			if ref := anyString(reportMap(row[key])["ref"]); strings.HasPrefix(ref, "actor:") {
				actorItems = append(actorItems, map[string]any{"actor_id": strings.TrimPrefix(ref, "actor:")})
			}
		}
	}
	if err := reader.prepareActivityActors(actorItems); err != nil {
		return nil, false, err
	}
	for _, row := range rows {
		for _, key := range []string{"owner", "next_actor"} {
			item := reportMap(row[key])
			if ref := anyString(item["ref"]); strings.HasPrefix(ref, "actor:") {
				item["label"] = reader.activityActorLabel(strings.TrimPrefix(ref, "actor:"))
			}
		}
	}
	// Resolve labels only for the admitted project set; invisible projects do not
	// contribute a group label or metadata to this reader.
	projectRefs := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		if ref := anyString(row["_project_ref"]); ref != "" && !seen[ref] {
			projectRefs = append(projectRefs, ref)
			seen[ref] = true
		}
	}
	labels := map[string]string{}
	if len(projectRefs) > 0 {
		facts, err := store.ReportActivitySubjects(reader.r.Context(), projectRefs, nil)
		if err != nil {
			return nil, false, err
		}
		for i, f := range facts {
			if f.Resolvable {
				labels[projectRefs[i]] = f.Title
			}
		}
	}
	counts := map[string]int{}
	states := []map[string]any{}
	rank := map[string]int{}
	for i, s := range collectionStates[q.Collection] {
		states = append(states, map[string]any{"key": s[0], "label": s[1], "tone": s[2]})
		counts[s[0]] = 0
		rank[s[0]] = i
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := anyString(rows[i]["state"]), anyString(rows[j]["state"])
		if rank[a] != rank[b] {
			return rank[a] < rank[b]
		}
		if rows[i]["updated_at"] != rows[j]["updated_at"] {
			return anyString(rows[i]["updated_at"]) > anyString(rows[j]["updated_at"])
		}
		return anyString(rows[i]["ref"]) < anyString(rows[j]["ref"])
	})
	groups := []map[string]any{}
	byGroup := map[string]int{}
	shown := 0
	limit := q.Limit
	if limit == 0 {
		limit = 24
	}
	for _, row := range rows {
		state := anyString(row["state"])
		counts[state]++
		selected := len(q.StatusKeys) == 0
		for _, s := range q.StatusKeys {
			selected = selected || s == state
		}
		if !selected {
			continue
		}
		if shown >= limit {
			partial = true
			continue
		}
		shown++
		key, label := "all", "All"
		if q.GroupBy != "none" {
			ref := anyString(row["_project_ref"])
			key, label = "none", "No project"
			if title := labels[ref]; title != "" {
				key, label = "project:"+strings.TrimPrefix(ref, "topic:"), title
			}
		}
		delete(row, "_project_ref")
		index, ok := byGroup[key]
		if !ok {
			index = len(groups)
			byGroup[key] = index
			groups = append(groups, map[string]any{"key": key, "label": label, "rows": []map[string]any{}})
		}
		groups[index]["rows"] = append(groups[index]["rows"].([]map[string]any), row)
	}
	return map[string]any{"collection": q.Collection, "states": states, "groups": groups, "counts": counts, "has_more": partial}, partial, nil
}

func (reader *reportReader) collectionLoop(row, loop map[string]any) {
	health := anyString(loop["health"])
	state := map[string]string{"failing": "failing", "overdue": "stopped_reporting", "stalled": "flat", "healthy": "improving", "unknown": "unknown"}[health]
	if state == "" {
		state = "unknown"
	}
	row["state"] = state
	row["subtitle"] = nullableText(anyString(loop["health_reason"]))
	goal := reportMap(loop["goal"])
	points := []map[string]any{}
	if all, ok := goal["points"].([]map[string]any); ok {
		for _, p := range all {
			at, _ := time.Parse(time.RFC3339Nano, anyString(p["at"]))
			if !at.Before(reader.now.UTC().Truncate(24*time.Hour).Add(-13*24*time.Hour)) && !at.After(reader.now) {
				points = append(points, map[string]any{"t": p["at"], "v": p["value"]})
			}
		}
	}
	label := firstNonEmptyString(anyString(goal["label"]), anyString(goal["metric"]))
	if latest := goal["latest"]; latest != nil {
		value := fmt.Sprint(latest)
		if n, ok := latest.(float64); ok {
			value = fmt.Sprintf("%.4g", n)
		}
		label = strings.TrimSpace(fmt.Sprintf("%s %s %s", label, value, anyString(goal["unit"])))
	}
	if len(points) > 0 {
		row["progress"] = map[string]any{"kind": "spark", "points": points, "label": label, "direction": goal["direction"]}
	} else {
		row["progress"] = map[string]any{"kind": "label", "label": firstNonEmptyString(label, "No metric")}
	}
	last := loop["last_run_at"]
	at := anyString(last)
	if p, ok := last.(*string); ok {
		at = stringPointer(p)
	}
	tone := "neutral"
	if health == "overdue" {
		tone = "warning"
	}
	row["time"] = map[string]any{"label": fmt.Sprintf("Last run · cadence %vs", loop["every_seconds"]), "at": nullableText(at), "tone": tone}
}
