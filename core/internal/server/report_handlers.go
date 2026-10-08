package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	reports "agent-nexus-visualreport"
)

func handleRenderReport(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, 503, "unavailable", "report data is unavailable")
		return
	}
	id, ok := resolveHTTPResourceID(w, r, opts, "document", documentID, "document")
	if !ok {
		return
	}
	doc, revision, err := opts.primitiveStore.GetDocument(r.Context(), id)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, 404, "not_found", "document not found")
		} else {
			writeError(w, 503, "unavailable", "report document is unavailable")
		}
		return
	}
	if !inboxItemAccessible(r, opts, documentBackingThreadID(doc), nil) {
		denyPMNotFound(w, "document")
		return
	}
	panels, err := reports.ParseAll(revision["content"])
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	observedAt, results := materializeReportPanels(r, opts, panels)
	if err := checkReportReviews(r, opts, doc, revision, panels); err != nil {
		writeError(w, 503, "unavailable", "report review reminders are unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"document_ref": doc["ref"], "revision_ref": revision["ref"], "observed_at": observedAt, "panels": results})
}

// File previews use the same bounded, permission-filtered materializer as saved
// reports without creating a document or revision as a side effect.
func handlePreviewReport(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	var req struct {
		Report json.RawMessage `json:"report"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if len(req.Report) == 0 || string(req.Report) == "null" {
		writeError(w, http.StatusBadRequest, "invalid_request", "report is required")
		return
	}
	var content any
	if err := json.Unmarshal(req.Report, &content); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "report must be valid JSON")
		return
	}
	panels, err := reports.ParseAll(content)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	observedAt, results := materializeReportPanels(r, opts, panels)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"observed_at": observedAt, "panels": results})
}

func materializeReportPanels(r *http.Request, opts handlerOptions, panels []reports.Panel) (string, []map[string]any) {
	now := time.Now().UTC()
	reader := reportReader{r: r, opts: opts, now: now, visibility: map[string]bool{}}
	for _, panel := range panels {
		if panel.Type == "live-activity" {
			reader.decisionLimit = max(reader.decisionLimit, min(maxReportCandidates, panel.Query.Limit+1))
		}
	}
	results := []map[string]any{}
	for _, panel := range panels {
		if !reports.IsLive(panel.Type) && panel.Source == nil {
			due, _ := time.Parse(time.RFC3339Nano, panel.ReviewBy)
			results = append(results, map[string]any{"id": panel.ID, "type": panel.Type, "status": "ok", "observed_at": panel.AuthoredAt, "truncated": false, "data": panel.StaticData, "provenance_class": "authored", "author": panel.Author, "authored_at": panel.AuthoredAt, "review_by": panel.ReviewBy, "review_by_defaulted": panel.ReviewByDefaulted, "review_due": !now.Before(due)})
			continue
		}
		if panel.Source != nil {
			result := reader.materializeSeries(panel)
			result["provenance_class"] = "live"
			results = append(results, result)
			continue
		}
		data, truncated, err := reader.materialize(panel)
		result := map[string]any{"id": panel.ID, "type": panel.Type, "status": "ok", "observed_at": now.Format(time.RFC3339Nano), "truncated": truncated, "data": data, "provenance_class": "live"}
		if err != nil {
			result["status"] = "unavailable"
			result["data"] = map[string]any{}
			result["message"] = "This panel could not be read with your current access. Try again or check the source."
		}
		results = append(results, result)
	}
	return now.Format(time.RFC3339Nano), results
}

// Reads are shared across panels, scoped to this request and principal. Nothing is
// cached between requests or written back into the report document.
const maxReportCandidates = 200

type reportReader struct {
	decisionLimit    int
	r                *http.Request
	opts             handlerOptions
	now              time.Time
	labels           map[string]string
	actorLabels      map[string]string
	visibility       map[string]bool
	hidden           map[string]bool
	work             []map[string]any
	boards           map[string]map[string]any
	workScopes       map[string]reportWorkRead
	workPartial      bool
	workErr          error
	events           []map[string]any
	eventsRead       bool
	eventsPartial    bool
	eventsErr        error
	decisionsRead    bool
	decisions        []map[string]any
	decisionsPartial bool
	decisionsErr     error
}

func reportActive(row map[string]any) bool {
	state := anyString(row["state"])
	return row != nil && (state == "" || state == "active") && anyString(row["archived_at"]) == "" && anyString(row["trashed_at"]) == ""
}

type reportWorkStore interface {
	ListReportWork(context.Context, primitives.ReportWorkFilter) (primitives.ReportWorkPage, error)
}
type reportWorkRead struct {
	work    []map[string]any
	partial bool
	err     error
}

func (reader *reportReader) loadWork(filter primitives.ReportWorkFilter) {
	keyBytes, _ := json.Marshal(filter)
	key := string(keyBytes)
	if cached, ok := reader.workScopes[key]; ok {
		reader.work, reader.workPartial, reader.workErr = cached.work, cached.partial, cached.err
		return
	}
	// Keep all scope reads request-local, so panels with identical scopes share
	// the bounded query, while different scopes cannot consume each other's cap.
	reader.work, reader.workPartial, reader.workErr = nil, false, nil
	if reader.boards == nil {
		reader.boards = map[string]map[string]any{}
	}
	store, ok := reader.opts.primitiveStore.(reportWorkStore)
	if !ok {
		reader.workErr = fmt.Errorf("work unavailable")
	} else {
		page, err := store.ListReportWork(reader.r.Context(), filter)
		reader.workPartial, reader.workErr = page.Truncated, err
		for _, work := range page.Work {
			ref := anyString(work["board_ref"])
			board, ok := page.Boards[ref]
			if !ok {
				reader.workErr = fmt.Errorf("report board context unavailable")
				break
			}
			reader.boards[ref] = map[string]any{"title": board.Title}
			// Both owners were joined by the batch read; no per-card thread or
			// board hydration is needed to enforce the same privacy rule.
			if !reportActive(work) || !canAccessPMThread(reader.r, reader.opts, map[string]any{"pm_actor_id": board.PrivateOwner}) || !canAccessPMThread(reader.r, reader.opts, map[string]any{"pm_actor_id": page.PrivateOwners[anyString(work["id"])]}) {
				continue
			}
			phase := anyString(work["phase"])
			if !filter.IncludeClosed && (phase == "done" || phase == "cancelled") {
				continue
			}
			reader.work = append(reader.work, work)
		}
	}
	if reader.workScopes == nil {
		reader.workScopes = map[string]reportWorkRead{}
	}
	reader.workScopes[key] = reportWorkRead{reader.work, reader.workPartial, reader.workErr}
}

func (reader *reportReader) scopedWork(q reports.Query, includeClosed bool) ([]map[string]any, error) {
	filter := primitives.ReportWorkFilter{Limit: maxReportCandidates, IncludeClosed: includeClosed}
	selected := map[string]bool{}
	for _, ref := range q.BoardRefs {
		resolved, err := reader.opts.primitiveStore.ResolveResourceRef(reader.r.Context(), primitives.ResourceRefInput{Type: "board", Ref: ref})
		if err != nil {
			return nil, err
		}
		board, err := reader.opts.primitiveStore.GetBoard(reader.r.Context(), resolved.ID)
		if err != nil {
			return nil, err
		}
		if !threadAccessible(reader.r, reader.opts, anyString(board["thread_id"])) {
			return nil, fmt.Errorf("board unavailable")
		}
		selected[anyString(board["ref"])] = true
		filter.BoardIDs = append(filter.BoardIDs, resolved.ID)
	}
	project := q.ProjectRef
	if project != "" {
		resolved, err := reader.opts.primitiveStore.ResolveResourceRef(reader.r.Context(), primitives.ResourceRefInput{Type: "topic", Ref: project})
		if err != nil {
			return nil, err
		}
		topic, err := reader.opts.primitiveStore.GetTopic(reader.r.Context(), resolved.ID)
		if err != nil || !threadAccessible(reader.r, reader.opts, anyString(topic["thread_id"])) {
			return nil, fmt.Errorf("project unavailable")
		}
		project = anyString(topic["ref"])
	}
	filter.ProjectRef = project
	if q.CardRef != "" {
		resolved, err := reader.opts.primitiveStore.ResolveResourceRef(reader.r.Context(), primitives.ResourceRefInput{Type: "card", Ref: q.CardRef})
		if err != nil {
			return nil, err
		}
		if reader.visibility == nil {
			reader.visibility = map[string]bool{}
		}
		if !reader.activeRef(q.CardRef) {
			return nil, fmt.Errorf("card unavailable")
		}
		filter.CardID = resolved.ID
	}
	reader.loadWork(filter)
	if reader.workErr != nil {
		return nil, reader.workErr
	}
	out := []map[string]any{}
	for _, work := range reader.work {
		if len(selected) > 0 && !selected[anyString(work["board_ref"])] {
			continue
		}
		if project != "" && anyString(work["project_ref"]) != project {
			continue
		}
		out = append(out, work)
	}
	return out, nil
}

func (reader *reportReader) materialize(panel reports.Panel) (map[string]any, bool, error) {
	q := panel.Query
	switch panel.Type {
	case "live-initiatives", "live-work-mix", "live-cards":
		work, err := reader.scopedWork(q, panel.Type == "live-cards")
		if err != nil {
			return nil, false, err
		}
		if panel.Type == "live-work-mix" {
			counts := map[string]int{}
			for _, row := range work {
				key := anyString(row["phase"])
				if q.GroupBy == "board" {
					key = anyString(row["board_ref"])
				}
				if key == "" {
					key = "unknown"
				}
				counts[key]++
			}
			keys := []string{}
			for key := range counts {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			buckets := []map[string]any{}
			for _, key := range keys {
				label := strings.ReplaceAll(key, "_", " ")
				if q.GroupBy == "board" {
					label = anyString(reader.boards[key]["title"])
				}
				buckets = append(buckets, map[string]any{"key": key, "label": label, "count": counts[key]})
			}
			return map[string]any{"group_by": q.GroupBy, "total": len(work), "buckets": buckets}, reader.workPartial, nil
		}
		if panel.Type == "live-cards" {
			filtered := make([]map[string]any, 0, len(work))
			for _, row := range work {
				if q.Status != "" && anyString(row["phase"]) != q.Status {
					continue
				}
				if q.Label != "" && !reportHasValue(row, q.Label, "labels") {
					continue
				}
				if q.Role != "" && !reportHasValue(row, q.Role, "roles") {
					continue
				}
				filtered = append(filtered, row)
			}
			work = filtered
		}
		summaryFormat := reader.r.URL.Query().Get("summary") == "1"
		if store, ok := reader.opts.primitiveStore.(planStore); ok && (panel.Type == "live-initiatives" || summaryFormat) {
			ctx := reader.r.Context()
			if !summaryFormat {
				ctx = primitives.WithLegacyCardPlans(ctx)
			}
			if err := store.EnrichCardPlans(ctx, work, planVisibility(reader.r, reader.opts), reader.now, planStalledAfter()); err != nil {
				return nil, false, err
			}
		}
		sort.SliceStable(work, func(i, j int) bool {
			if q.Sort == "title" && anyString(work[i]["title"]) != anyString(work[j]["title"]) {
				return anyString(work[i]["title"]) < anyString(work[j]["title"])
			}
			if q.Sort == "priority" {
				a, b := anyString(work[i]["priority"]), anyString(work[j]["priority"])
				if a == "" {
					a = "z"
				}
				if b == "" {
					b = "z"
				}
				if a != b {
					return a < b
				}
			}
			a, _ := time.Parse(time.RFC3339Nano, anyString(work[i]["updated_at"]))
			b, _ := time.Parse(time.RFC3339Nano, anyString(work[j]["updated_at"]))
			if !a.Equal(b) {
				return a.After(b)
			}
			return anyString(work[i]["ref"]) < anyString(work[j]["ref"])
		})
		partial := reader.workPartial || len(work) > q.Limit
		items := []map[string]any{}
		for i, row := range work {
			if i >= q.Limit {
				break
			}
			summary, progress, needs := reports.Summary(anyString(row["summary"]))
			item := map[string]any{"ref": row["ref"], "title": row["title"], "summary": summary, "progress": progress, "needs": needs, "priority": anyString(row["priority"]), "phase": row["phase"], "board_ref": row["board_ref"], "updated_at": row["updated_at"]}
			if summaryFormat {
				item["work_summary"], item["summary"], item["summary_text"] = row["work_summary"], row["work_summary"], summary
				item["plan_step_digest"] = row["plan_step_digest"]
			}
			// Keep the authored plan and ownership fields from the same bounded card
			// projection. Plan refs are carried by plan.steps[].ref; no N+1 reads.
			for _, key := range []string{"plan", "plan_state", "plan_health", "next_step", "status_mismatch", "plan_resolution_truncated", "source_refs", "assignee_refs"} {
				if value, ok := row[key]; ok && value != nil {
					item[key] = value
				}
			}
			if health, ok := row["plan_health"].(plans.Health); ok {
				item["health"] = plans.LegacyHealth(health.State)
			}
			if state, ok := row["plan_state"].(plans.State); ok {
				item["progress"] = state.Progress
				item["plan_state"] = state
				item["health"] = state.Health
				blocked := map[string]bool{}
				for _, step := range state.Steps {
					if step.Status == "blocked" {
						blocked[step.ID] = true
					}
				}
				needs = []string{}
				if p, ok := row["plan"].(plans.Plan); ok {
					for _, step := range p.Steps {
						if blocked[step.ID] {
							needs = append(needs, step.Title)
						}
					}
				}
				item["needs"] = needs
			}
			items = append(items, item)
		}
		return map[string]any{"items": items}, partial, nil
	case "live-asks":
		return reader.asks(q)
	case "live-activity":
		return reader.activity(q)
	case "live-fleet-health":
		return reader.fleetHealth()
	}
	return nil, false, fmt.Errorf("unknown panel type")
}

func (reader *reportReader) loadEvents() {
	if reader.eventsRead {
		return
	}
	reader.eventsRead = true
	if _, canonical := reader.opts.primitiveStore.(*primitives.Store); !canonical {
		if store, ok := reader.opts.primitiveStore.(interface {
			HiddenSubjectRefs(context.Context) (map[string]bool, error)
		}); ok {
			var err error
			reader.hidden, err = store.HiddenSubjectRefs(reader.r.Context())
			if err != nil {
				reader.eventsErr = err
				return
			}
		}
	}
	cursor := ""
	for count := 0; count < maxReportCandidates; {
		page, err := reader.opts.primitiveStore.ListEventsPage(reader.r.Context(), primitives.EventListFilter{ReportSubjects: true, Types: []string{"human_attention_requested", "human_attention_responded", "card_moved", "card_resolved", "card_closed", "card_updated"}, Limit: 200, Cursor: cursor})
		if err != nil {
			reader.eventsErr = err
			return
		}
		count += len(page.Events)
		reader.events = append(reader.events, filterAccessibleEvents(reader.r, reader.opts, page.Events)...)
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if store, ok := reader.opts.primitiveStore.(*primitives.Store); ok {
		refs := []string{}
		for _, event := range reader.events {
			refs = append(refs, stringSliceAny(event["refs"])...)
			if id := eventThreadID(event); id != "" {
				refs = append(refs, "thread:"+id)
			}
			if payload, ok := event["payload"].(map[string]any); ok {
				if ref := anyString(payload["subject_ref"]); ref != "" {
					refs = append(refs, ref)
				}
			}
		}
		var err error
		reader.hidden, err = store.HiddenSubjectRefsFor(reader.r.Context(), refs)
		if err != nil {
			reader.eventsErr = err
			return
		}
		if err := reader.prepareCardRefs(refs); err != nil {
			reader.eventsErr = err
			return
		}

	}

	reader.eventsPartial = cursor != ""
}

// Hide archived subjects and PM history using the same resource and thread rules
// as ordinary reads. Canonical refs may be handles or internal historical ids.
func (reader *reportReader) activeRef(ref string) bool {
	if reader.hidden[ref] {
		return false
	}
	if allowed, ok := reader.visibility[ref]; ok {
		return allowed
	}
	kind, value, found := strings.Cut(ref, ":")
	if !found {
		return false
	}
	var row map[string]any
	var err error
	if kind == "board" || kind == "topic" || kind == "card" || kind == "thread" {
		resolved, resolveErr := reader.opts.primitiveStore.ResolveResourceRef(reader.r.Context(), primitives.ResourceRefInput{Type: kind, Ref: ref})
		if resolveErr != nil {
			reader.visibility[ref] = false
			return false
		}
		value = resolved.ID
	}
	switch kind {
	case "board":
		if store, ok := reader.opts.primitiveStore.(*primitives.Store); ok {
			row, err = store.ReportSubjectSnapshot(reader.r.Context(), kind, value)
		} else {
			row, err = reader.opts.primitiveStore.GetBoard(reader.r.Context(), value)
		}
	case "topic":
		if store, ok := reader.opts.primitiveStore.(*primitives.Store); ok {
			row, err = store.ReportSubjectSnapshot(reader.r.Context(), kind, value)
		} else {
			row, err = reader.opts.primitiveStore.GetTopic(reader.r.Context(), value)
		}
	case "card":
		store, ok := reader.opts.primitiveStore.(WorkStore)
		if !ok {
			return false
		}
		row, err = store.GetWork(reader.r.Context(), value)
		if err == nil {
			for _, parent := range []string{anyString(row["board_ref"]), anyString(row["project_ref"])} {
				if parent != "" && !reader.activeRef(parent) {
					reader.visibility[ref] = false
					return false
				}
			}
		}
	case "thread":
		row, err = reader.opts.primitiveStore.GetThread(reader.r.Context(), value)
	default:
		return true
	}
	allowed := err == nil && reportActive(row) && threadAccessible(reader.r, reader.opts, anyString(row["thread_id"]))
	reader.visibility[ref] = allowed
	if allowed {
		if reader.labels == nil {
			reader.labels = map[string]string{}
		}
		reader.labels[ref] = anyString(row["title"])
	}
	return allowed
}

func (reader *reportReader) activeEvent(event map[string]any) bool {
	refs := stringSliceAny(event["refs"])
	if thread := eventThreadID(event); thread != "" {
		refs = append(refs, "thread:"+thread)
	}
	payload, _ := event["payload"].(map[string]any)
	if subject := anyString(payload["subject_ref"]); subject != "" {
		refs = append(refs, subject)
	}
	refs = append(refs, stringSliceAny(payload["related_refs"])...)
	for _, ref := range refs {
		if !reader.activeRef(ref) {
			return false
		}
	}
	return true
}

func (reader *reportReader) asks(q reports.Query) (map[string]any, bool, error) {
	reader.loadEvents()
	if reader.eventsErr != nil {
		return nil, false, reader.eventsErr
	}
	answered := map[string]map[string]any{}
	for _, event := range reader.events {
		if anyString(event["type"]) != "human_attention_responded" {
			continue
		}
		payload, _ := event["payload"].(map[string]any)
		for _, ref := range stringSliceAny(event["refs"]) {
			if strings.HasPrefix(ref, "inbox:") {
				id := strings.TrimPrefix(ref, "inbox:")
				if answered[id] == nil {
					answered[id] = event
				}
			}
		}
		if id := anyString(payload["inbox_item_id"]); id != "" && answered[id] == nil {
			answered[id] = event
		}
	}
	items := []map[string]any{}
	for _, event := range reader.events {
		if anyString(event["type"]) != "human_attention_requested" || !reader.activeEvent(event) {
			continue
		}
		request, ok := deriveHumanAttentionInboxItem(event)
		if !ok {
			continue
		}
		row := request.Data
		if q.CardRef != "" && anyString(row["subject_ref"]) != q.CardRef {
			continue
		}
		response := answered[request.ID]
		if q.AnsweredOnly && response == nil {
			continue
		}
		status := "open"
		if response != nil {
			at, _ := time.Parse(time.RFC3339Nano, anyString(response["ts"]))
			if (!q.IncludeAnswered && !q.AnsweredOnly) || at.Before(reader.now.Add(-time.Duration(q.AnsweredWithinHours)*time.Hour)) {
				continue
			}
			status = "answered"
			row["inbox_item_id"] = request.ID
			row["id"] = "completed:" + anyString(response["id"])
			payload, _ := response["payload"].(map[string]any)
			row["response_text"] = anyString(payload["response_text"])
			row["responded_at"] = response["ts"]
		}
		row["status"] = status
		row["age_seconds"] = int64(max(0, reader.now.Sub(request.TriggerAt).Seconds()))
		items = append(items, row)
	}
	// Oldest open asks first; recent answers come after open asks.
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a["status"] != b["status"] {
			return a["status"] == "open"
		}
		field := "created_at"
		if a["status"] == "answered" {
			field = "responded_at"
		}
		left, _ := time.Parse(time.RFC3339Nano, anyString(a[field]))
		right, _ := time.Parse(time.RFC3339Nano, anyString(b[field]))
		if left.Equal(right) {
			return anyString(a["id"]) < anyString(b["id"])
		}
		if field == "responded_at" {
			return left.After(right)
		}
		return left.Before(right)
	})
	partial := reader.eventsPartial || len(items) > q.Limit
	if len(items) > q.Limit {
		items = items[:q.Limit]
	}
	return map[string]any{"items": items}, partial, nil
}

func (reader *reportReader) activity(q reports.Query) (map[string]any, bool, error) {
	reader.loadEvents()
	if reader.eventsErr != nil {
		return nil, false, reader.eventsErr
	}
	items := []map[string]any{}
	bursts := map[string]int{}
	for _, event := range reader.events {
		if !reader.activeEvent(event) {
			continue
		}
		kind := anyString(event["type"])
		actor := anyString(event["actor_id"])
		at, _ := time.Parse(time.RFC3339Nano, anyString(event["ts"]))
		summary := anyString(event["summary"])
		if kind == "card_updated" {
			board := ""
			for _, ref := range stringSliceAny(event["refs"]) {
				if strings.HasPrefix(ref, "board:") {
					board = ref
					break
				}
			}
			if board == "" {
				continue
			}
			key := actor + "/" + board
			if index, ok := bursts[key]; ok {
				newest, _ := time.Parse(time.RFC3339Nano, anyString(items[index]["ts"]))
				if newest.Sub(at) <= 5*time.Minute {
					items[index]["count"] = items[index]["count"].(int) + 1
					label := reader.labels[board]
					if label == "" {
						label = "board"
					}
					if reader.actorLabels == nil {
						reader.actorLabels = map[string]string{}
					}
					actorLabel, known := reader.actorLabels[actor]
					if !known {
						actorLabel = actor
						if reader.opts.actorRegistry != nil {
							if known, err := reader.opts.actorRegistry.Get(reader.r.Context(), actor); err == nil && known.DisplayName != "" {
								actorLabel = known.DisplayName
							}
						}
						reader.actorLabels[actor] = actorLabel
					}
					items[index]["summary"] = fmt.Sprintf("%s updated %s · %d edits", actorLabel, label, items[index]["count"])
					continue
				}
			}
			bursts[key] = len(items)
		}
		items = append(items, map[string]any{"ref": event["ref"], "type": kind, "summary": summary, "actor_id": actor, "ts": event["ts"], "count": 1})
	}
	decisions, decisionsPartial, err := reader.decisionActivity()
	if err != nil {
		return nil, false, err
	}
	items = append(items, decisions...)
	sort.SliceStable(items, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339Nano, anyString(items[i]["ts"]))
		right, _ := time.Parse(time.RFC3339Nano, anyString(items[j]["ts"]))
		if left.Equal(right) {
			return anyString(items[i]["ref"]) > anyString(items[j]["ref"])
		}
		return left.After(right)
	})
	partial := reader.eventsPartial || decisionsPartial || len(items) > q.Limit
	if len(items) > q.Limit {
		items = items[:q.Limit]
	}
	return map[string]any{"items": items}, partial, nil
}

// PM decisions are durable records rather than native events. Use the ordinary
// permission-filtered service read; never inspect private conversation bodies.
func (reader *reportReader) decisionActivity() ([]map[string]any, bool, error) {
	if !reader.decisionsRead {
		reader.decisionsRead = true
		reader.decisions, reader.decisionsPartial, reader.decisionsErr = reader.readDecisionActivity()
	}
	return reader.decisions, reader.decisionsPartial, reader.decisionsErr
}

func (reader *reportReader) readDecisionActivity() ([]map[string]any, bool, error) {
	items := []map[string]any{}
	runtime := reader.opts.pmRuntime
	if runtime == nil || runtime.Service == nil {
		return items, false, nil
	}
	principal, ok := cachedAuthenticatedPrincipal(reader.r)
	if !ok || principal == nil {
		return nil, false, pm.ErrForbidden
	}
	p := pm.Principal{WorkspaceID: runtime.cfg.PM.WorkspaceID, ActorID: principal.ActorID, Human: principal.PrincipalKind == string(auth.PrincipalKindHuman)}
	limit := reader.decisionLimit
	if limit <= 0 {
		limit = maxReportCandidates
	}
	cursor := ""
	for count := 0; count < limit; {
		page, err := runtime.Service.DecisionPage(reader.r.Context(), p, limit, cursor)
		if err != nil {
			return nil, false, err
		}
		count += len(page.Items)
		refs := []string{}
		for _, d := range page.Items {
			refs = append(refs, d.WorkRef)
		}
		if err := reader.prepareCardRefs(refs); err != nil {
			return nil, false, err
		}
		for _, decision := range page.Items {
			if !reader.activeRef(decision.WorkRef) {
				continue
			}
			label := reader.labels[decision.WorkRef]
			if label == "" {
				label = "work"
			}
			items = append(items, map[string]any{"ref": "decision:" + decision.ID, "type": "decision_proposed", "summary": "Decision requested for " + label, "actor_id": decision.ProposedBy, "ts": decision.CreatedAt.Format(time.RFC3339Nano), "count": 1})
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	return items, cursor != "", nil
}

func reportHasValue(row map[string]any, value string, keys ...string) bool {
	for _, key := range keys {
		for _, candidate := range stringSliceAny(row[key]) {
			if candidate == value {
				return true
			}
		}
		if object, ok := row[key].(map[string]any); ok {
			for _, candidate := range object {
				if anyString(candidate) == value {
					return true
				}
			}
		}
	}
	return false
}

// Batch card projections while preserving the same active parent checks and
// source title overlays as activeRef. Other stores retain the point-read path.
func (reader *reportReader) prepareCardRefs(refs []string) error {
	store, canonical := reader.opts.primitiveStore.(*primitives.Store)
	_, scoped := resourceaccess.PolicyFrom(reader.r.Context())
	if !canonical || !scoped {
		return nil
	}
	pending := []string{}
	seen := map[string]bool{}
	for _, ref := range refs {
		if strings.HasPrefix(ref, "card:") && !seen[ref] {
			if _, cached := reader.visibility[ref]; !cached {
				pending = append(pending, ref)
				seen[ref] = true
			}
		}
	}
	if reader.visibility == nil {
		reader.visibility = map[string]bool{}
	}
	if reader.labels == nil {
		reader.labels = map[string]string{}
	}
	for len(pending) > 0 {
		n := min(200, len(pending))
		batch := pending[:n]
		pending = pending[n:]
		snapshots, err := store.ReportWorkSnapshots(reader.r.Context(), batch)
		if err != nil {
			return err
		}
		for _, ref := range batch {
			w, ok := snapshots[ref]
			allowed := ok && reportActive(w)
			if allowed {
				for _, parent := range []string{anyString(w["board_ref"]), anyString(w["project_ref"])} {
					if parent != "" && !reader.activeRef(parent) {
						allowed = false
						break
					}
				}
			}
			reader.visibility[ref] = allowed
			if allowed {
				reader.labels[ref] = anyString(w["title"])
			}
		}
	}
	return nil
}
