package server

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/reports"
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
	if !requireAccessibleThreadID(w, r, opts, documentBackingThreadID(doc), "document") {
		return
	}
	panels, err := reports.Parse(revision["content"])
	if err != nil {
		writeError(w, 400, "invalid_request", err.Error())
		return
	}
	now := time.Now().UTC()
	reader := reportReader{r: r, opts: opts, now: now, visibility: map[string]bool{}}
	results := []map[string]any{}
	for _, panel := range panels {
		data, truncated, err := reader.materialize(panel)
		result := map[string]any{"id": panel.ID, "type": panel.Type, "status": "ok", "observed_at": now.Format(time.RFC3339Nano), "truncated": truncated, "data": data}
		if err != nil {
			result["status"] = "unavailable"
			result["data"] = map[string]any{}
			result["message"] = "This panel could not be read with your current access. Try again or check the source."
		}
		results = append(results, result)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"document_ref": doc["ref"], "revision_ref": revision["ref"], "observed_at": now.Format(time.RFC3339Nano), "panels": results})
}

// Reads are shared across panels, scoped to this request and principal. Nothing is
// cached between requests or written back into the report document.
type reportReader struct {
	r                *http.Request
	opts             handlerOptions
	now              time.Time
	labels           map[string]string
	visibility       map[string]bool
	work             []map[string]any
	boards           map[string]map[string]any
	workRead         bool
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

func (reader *reportReader) loadWork() {
	if reader.workRead {
		return
	}
	reader.workRead = true
	reader.boards = map[string]map[string]any{}
	limit := 1000
	boards, cursor, err := reader.opts.primitiveStore.ListBoards(reader.r.Context(), primitives.BoardListFilter{States: []string{"active"}, Limit: &limit})
	if err != nil {
		reader.workErr = err
		return
	}
	reader.workPartial = cursor != ""
	for _, b := range boards {
		if reportActive(b.Board) && threadAccessible(reader.r, reader.opts, anyString(b.Board["thread_id"])) {
			reader.boards[anyString(b.Board["ref"])] = b.Board
		}
	}
	store, ok := reader.opts.primitiveStore.(WorkStore)
	if !ok {
		reader.workErr = fmt.Errorf("work unavailable")
		return
	}
	cursor = ""
	for count := 0; count < reports.MaxRows; {
		page, err := store.ListWork(reader.r.Context(), primitives.WorkListFilter{Limit: 200, Cursor: cursor})
		if err != nil {
			reader.workErr = err
			return
		}
		count += len(page.Work)
		for _, work := range page.Work {
			if reader.boards[anyString(work["board_ref"])] == nil || !reportActive(work) || !threadAccessible(reader.r, reader.opts, anyString(work["thread_id"])) {
				continue
			}
			phase := anyString(work["phase"])
			if phase == "done" || phase == "cancelled" {
				continue
			}
			reader.work = append(reader.work, work)
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	reader.workPartial = reader.workPartial || cursor != ""
}

func (reader *reportReader) scopedWork(q reports.Query) ([]map[string]any, error) {
	reader.loadWork()
	if reader.workErr != nil {
		return nil, reader.workErr
	}
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
	case "live-initiatives", "live-work-mix":
		work, err := reader.scopedWork(q)
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
			items = append(items, map[string]any{"ref": row["ref"], "title": row["title"], "summary": summary, "progress": progress, "needs": needs, "priority": anyString(row["priority"]), "phase": row["phase"], "board_ref": row["board_ref"], "updated_at": row["updated_at"]})
		}
		return map[string]any{"items": items}, partial, nil
	case "live-asks":
		return reader.asks(q)
	case "live-activity":
		return reader.activity(q)
	}
	return nil, false, fmt.Errorf("unknown panel type")
}

func (reader *reportReader) loadEvents() {
	if reader.eventsRead {
		return
	}
	reader.eventsRead = true
	cursor := ""
	for count := 0; count < reports.MaxRows; {
		page, err := reader.opts.primitiveStore.ListEventsPage(reader.r.Context(), primitives.EventListFilter{Types: []string{"human_attention_requested", "human_attention_responded", "card_moved", "card_resolved", "card_closed", "card_updated"}, Limit: 200, Cursor: cursor})
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
	reader.eventsPartial = cursor != ""
}

// Hide archived subjects and PM history using the same resource and thread rules
// as ordinary reads. Canonical refs may be handles or internal historical ids.
func (reader *reportReader) activeRef(ref string) bool {
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
		row, err = reader.opts.primitiveStore.GetBoard(reader.r.Context(), value)
	case "topic":
		row, err = reader.opts.primitiveStore.GetTopic(reader.r.Context(), value)
	case "card":
		store, ok := reader.opts.primitiveStore.(WorkStore)
		if !ok {
			return false
		}
		row, err = store.GetWork(reader.r.Context(), value)
		if err == nil {
			if board := anyString(row["board_ref"]); board != "" && !reader.activeRef(board) {
				return false
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
		response := answered[request.ID]
		status := "open"
		if response != nil {
			at, _ := time.Parse(time.RFC3339Nano, anyString(response["ts"]))
			if !q.IncludeAnswered || at.Before(reader.now.Add(-time.Duration(q.AnsweredWithinHours)*time.Hour)) {
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
					actorLabel := actor
					if reader.opts.actorRegistry != nil {
						if known, err := reader.opts.actorRegistry.Get(reader.r.Context(), actor); err == nil && known.DisplayName != "" {
							actorLabel = known.DisplayName
						}
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
	cursor := ""
	for count := 0; count < reports.MaxRows; {
		page, err := runtime.Service.DecisionPage(reader.r.Context(), p, 200, cursor)
		if err != nil {
			return nil, false, err
		}
		count += len(page.Items)
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
