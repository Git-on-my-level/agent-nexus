package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

const defaultInboxRiskHorizon = 7 * 24 * time.Hour

type derivedInboxItem struct {
	Data      map[string]any
	Category  string
	ID        string
	TriggerAt time.Time
	DueAt     time.Time
	HasDueAt  bool
}

func payloadFromDerivedInboxItem(item primitives.DerivedInboxItem) map[string]any {
	m := cloneWorkspaceMap(item.Data)
	if m == nil {
		m = map[string]any{}
	}
	trigger := strings.TrimSpace(item.TriggerAt)
	if trigger != "" {
		if _, ok := m["source_event_time"]; !ok {
			m["source_event_time"] = trigger
		}
		if _, ok := m["trigger_at"]; !ok {
			m["trigger_at"] = trigger
		}
	}
	return m
}

func payloadFromLocalDerivedInboxItem(item derivedInboxItem) map[string]any {
	m := cloneWorkspaceMap(item.Data)
	if m == nil {
		m = map[string]any{}
	}
	if item.TriggerAt.IsZero() {
		return m
	}
	trigger := item.TriggerAt.Format(time.RFC3339Nano)
	if _, ok := m["source_event_time"]; !ok {
		m["source_event_time"] = trigger
	}
	if _, ok := m["trigger_at"]; !ok {
		m["trigger_at"] = trigger
	}
	return m
}

func handleGetInbox(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	now := time.Now().UTC()
	horizon, ok := resolveInboxRiskHorizon(w, r, opts)
	if !ok {
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("risk_horizon_days")) != "" {
		items, err := deriveInboxItems(r.Context(), opts, now, horizon)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to derive inbox items")
			return
		}

		payloadItems := make([]map[string]any, 0, len(items))
		for _, item := range items {
			payloadItems = append(payloadItems, payloadFromLocalDerivedInboxItem(item))
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"items":        payloadItems,
			"generated_at": now.Format(time.RFC3339Nano),
		})
		return
	}

	threads, _, err := opts.primitiveStore.ListThreads(r.Context(), primitives.ThreadListFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load threads")
		return
	}
	threadIDs := make([]string, 0, len(threads))
	for _, thread := range threads {
		threadIDs = append(threadIDs, anyString(thread["id"]))
	}
	states, err := loadThreadProjectionStates(r.Context(), opts, threadIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox projection status")
		return
	}

	projected, err := opts.primitiveStore.ListDerivedInboxItems(r.Context(), primitives.DerivedInboxListFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox projections")
		return
	}

	payloadItems := make([]map[string]any, 0, len(projected))
	for _, item := range projected {
		payloadItems = append(payloadItems, payloadFromDerivedInboxItem(item))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":                payloadItems,
		"generated_at":         now.Format(time.RFC3339Nano),
		"projection_freshness": aggregateThreadProjectionFreshness(states, threadIDs),
	})
}

func handleGetInboxItem(w http.ResponseWriter, r *http.Request, opts handlerOptions, inboxItemID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	inboxItemID = strings.TrimSpace(inboxItemID)
	if inboxItemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "inbox_item_id is required")
		return
	}

	now := time.Now().UTC()
	horizon, ok := resolveInboxRiskHorizon(w, r, opts)
	if !ok {
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("risk_horizon_days")) != "" {
		items, err := deriveInboxItems(r.Context(), opts, now, horizon)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to derive inbox items")
			return
		}

		for _, item := range items {
			if strings.TrimSpace(item.ID) != inboxItemID {
				continue
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"item":         payloadFromLocalDerivedInboxItem(item),
				"generated_at": now.Format(time.RFC3339Nano),
			})
			return
		}
		writeError(w, http.StatusNotFound, "not_found", "inbox item not found")
		return
	}

	threads, _, err := opts.primitiveStore.ListThreads(r.Context(), primitives.ThreadListFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load threads")
		return
	}
	threadIDs := make([]string, 0, len(threads))
	for _, thread := range threads {
		threadIDs = append(threadIDs, anyString(thread["id"]))
	}
	states, err := loadThreadProjectionStates(r.Context(), opts, threadIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox projection status")
		return
	}

	item, err := opts.primitiveStore.GetDerivedInboxItem(r.Context(), inboxItemID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "inbox item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox projections")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"item":                 payloadFromDerivedInboxItem(item),
		"generated_at":         now.Format(time.RFC3339Nano),
		"projection_freshness": cloneWorkspaceMap(states[item.ThreadID].Freshness),
	})
}

func handleRebuildDerived(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	maintainer := opts.projectionMaintainer
	if maintainer == nil {
		maintainer = NewProjectionMaintainer(ProjectionMaintainerConfig{
			PrimitiveStore:   opts.primitiveStore,
			Contract:         opts.contract,
			InboxRiskHorizon: opts.inboxRiskHorizon,
			SystemActorID:    "oar-core",
		})
	}
	if err := maintainer.RunFullRebuild(r.Context(), time.Now().UTC(), actorID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to rebuild derived views")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleAckInboxItem(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID     string `json:"actor_id"`
		ThreadID    string `json:"thread_id"`
		InboxItemID string `json:"inbox_item_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	req.ThreadID = strings.TrimSpace(req.ThreadID)
	if req.ThreadID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "thread_id is required")
		return
	}

	req.InboxItemID = strings.TrimSpace(req.InboxItemID)
	if req.InboxItemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "inbox_item_id is required")
		return
	}

	event := map[string]any{
		"type":      "inbox_item_acknowledged",
		"thread_id": req.ThreadID,
		"refs":      []string{"inbox:" + req.InboxItemID},
		"summary":   "inbox item acknowledged",
		"payload": map[string]any{
			"inbox_item_id": req.InboxItemID,
		},
		"provenance": actorStatementProvenance(),
	}

	if err := validateEventReferenceConventions(opts.contract, event, []string{"inbox:" + req.InboxItemID}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	stored, err := opts.primitiveStore.AppendEvent(r.Context(), actorID, event)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to acknowledge inbox item")
		return
	}
	enqueueThreadProjectionsBestEffort(r.Context(), opts, []string{req.ThreadID}, time.Now().UTC())

	writeJSON(w, http.StatusCreated, map[string]any{"event": stored})
}

func deriveInboxItems(ctx context.Context, opts handlerOptions, now time.Time, riskHorizon time.Duration) ([]derivedInboxItem, error) {
	if _, err := emitStaleThreadExceptions(ctx, opts, now, ""); err != nil {
		return nil, err
	}

	return deriveInboxItemsNoStaleEmission(ctx, opts, now, riskHorizon)
}

func resolveInboxRiskHorizon(w http.ResponseWriter, r *http.Request, opts handlerOptions) (time.Duration, bool) {
	horizon := opts.inboxRiskHorizon
	if horizon <= 0 {
		horizon = defaultInboxRiskHorizon
	}

	if rawDays := strings.TrimSpace(r.URL.Query().Get("risk_horizon_days")); rawDays != "" {
		days, err := strconv.Atoi(rawDays)
		if err != nil || days < 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "risk_horizon_days must be a non-negative integer")
			return 0, false
		}
		horizon = time.Duration(days) * 24 * time.Hour
	}
	return horizon, true
}

func deriveInboxItemsNoStaleEmission(ctx context.Context, opts handlerOptions, now time.Time, riskHorizon time.Duration) ([]derivedInboxItem, error) {
	events, err := opts.primitiveStore.ListEvents(ctx, primitives.EventListFilter{
		Types: []string{"decision_needed", "intervention_needed", "exception_raised", "inbox_item_acknowledged", "receipt_added", "decision_made"},
	})
	if err != nil {
		return nil, err
	}

	ackedAt := latestInboxAcknowledgments(events)
	decidedIDs := decidedInboxItemIDs(events)
	latestActivity := latestThreadActivityFromEvents(events)
	items := make([]derivedInboxItem, 0)

	for _, event := range events {
		eventType, _ := event["type"].(string)
		switch eventType {
		case "decision_needed", "intervention_needed", "exception_raised":
			item, ok := deriveEventBackedInboxItem(event)
			if !ok {
				continue
			}
			if eventType == "exception_raised" && isStaleThreadException(event) {
				threadID, _ := event["thread_id"].(string)
				if activityAt, exists := latestActivity[threadID]; exists && activityAt.After(item.TriggerAt) {
					continue
				}
			}
			if isSuppressedByAck(item, ackedAt) {
				continue
			}
			if _, decided := decidedIDs[item.ID]; decided {
				continue
			}
			items = append(items, item)
		}
	}

	cards, err := opts.primitiveStore.ListCards(ctx, primitives.CardListFilter{})
	if err != nil {
		return nil, err
	}

	for _, card := range cards {
		item, ok := deriveWorkItemRiskInboxItem(card, now, riskHorizon)
		if !ok {
			continue
		}
		if isSuppressedByAck(item, ackedAt) {
			continue
		}
		if _, decided := decidedIDs[item.ID]; decided {
			continue
		}
		items = append(items, item)
	}

	sortInboxItems(items)
	return items, nil
}

func isStaleThreadException(event map[string]any) bool {
	payload, _ := event["payload"].(map[string]any)
	subtype, _ := payload["subtype"].(string)
	return subtype == "stale_thread"
}

func decidedInboxItemIDs(events []map[string]any) map[string]struct{} {
	out := make(map[string]struct{})
	for _, event := range events {
		eventType, _ := event["type"].(string)
		if eventType != "decision_made" {
			continue
		}
		refs, err := extractStringSlice(event["refs"])
		if err != nil {
			continue
		}
		for _, ref := range refs {
			prefix, value, err := schema.SplitTypedRef(ref)
			if err != nil || prefix != "inbox" {
				continue
			}
			out[value] = struct{}{}
		}
	}
	return out
}

func latestInboxAcknowledgments(events []map[string]any) map[string]time.Time {
	ackedAt := make(map[string]time.Time)
	for _, event := range events {
		eventType, _ := event["type"].(string)
		if eventType != "inbox_item_acknowledged" {
			continue
		}

		ts, ok := parseTimestamp(event["ts"])
		if !ok {
			continue
		}

		refs, err := extractStringSlice(event["refs"])
		if err != nil {
			continue
		}

		for _, ref := range refs {
			prefix, value, err := schema.SplitTypedRef(ref)
			if err != nil || prefix != "inbox" {
				continue
			}
			if current, exists := ackedAt[value]; !exists || ts.After(current) {
				ackedAt[value] = ts
			}
		}
	}
	return ackedAt
}

func deriveEventBackedInboxItem(event map[string]any) (derivedInboxItem, bool) {
	eventType, _ := event["type"].(string)
	threadID, _ := event["thread_id"].(string)
	sourceEventID, _ := event["id"].(string)
	triggerAt, ok := parseTimestamp(event["ts"])
	if strings.TrimSpace(threadID) == "" || strings.TrimSpace(sourceEventID) == "" || !ok {
		return derivedInboxItem{}, false
	}

	category := ""
	recommendedAction := ""
	titleFallback := ""
	switch eventType {
	case "decision_needed":
		category = "decision_needed"
		recommendedAction = "make_decision"
		titleFallback = "Decision needed"
	case "intervention_needed":
		category = "intervention_needed"
		recommendedAction = "take_action"
		titleFallback = "Intervention needed"
	case "exception_raised":
		category = "exception"
		recommendedAction = "investigate_exception"
		titleFallback = "Exception raised"
	default:
		return derivedInboxItem{}, false
	}

	title, _ := event["summary"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		title = titleFallback
	}

	id := makeInboxItemID(category, threadID, "", sourceEventID)
	data := map[string]any{
		"id":                 id,
		"category":           category,
		"thread_id":          threadID,
		"source_event_id":    sourceEventID,
		"title":              title,
		"recommended_action": recommendedAction,
	}

	return derivedInboxItem{
		Data:      data,
		Category:  category,
		ID:        id,
		TriggerAt: triggerAt,
	}, true
}

func deriveWorkItemRiskInboxItem(card map[string]any, now time.Time, riskHorizon time.Duration) (derivedInboxItem, bool) {
	threadID := strings.TrimSpace(firstNonEmptyString(card["parent_thread"], card["thread_id"]))
	cardID := strings.TrimSpace(anyString(card["id"]))
	if threadID == "" || cardID == "" {
		return derivedInboxItem{}, false
	}

	if !boardCardCountsAsOpenWorkItem(card) {
		return derivedInboxItem{}, false
	}

	riskState, dueAt, hasDueAt := boardCardRiskState(card, now, riskHorizon)
	if riskState == "" {
		return derivedInboxItem{}, false
	}

	triggerAt, ok := parseTimestamp(card["updated_at"])
	if !ok {
		triggerAt = now
	}

	title, _ := card["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Work item risk"
	}

	recommendedAction := "follow_up_work_item"
	switch riskState {
	case "overdue":
		recommendedAction = "resolve_overdue_work_item"
	case "blocked":
		recommendedAction = "unblock_work_item"
	}

	id := makeInboxItemID("work_item_risk", threadID, cardID, "")
	data := map[string]any{
		"id":                 id,
		"category":           "work_item_risk",
		"thread_id":          threadID,
		"card_id":            cardID,
		"board_id":           nullableStringValue(anyString(card["board_id"])),
		"title":              title,
		"risk_state":         riskState,
		"recommended_action": recommendedAction,
	}
	if hasDueAt {
		data["due_at"] = dueAt.Format(time.RFC3339)
	}

	return derivedInboxItem{
		Data:      data,
		Category:  "work_item_risk",
		ID:        id,
		TriggerAt: triggerAt,
		DueAt:     dueAt,
		HasDueAt:  hasDueAt,
	}, true
}

func boardCardCountsAsOpenWorkItem(card map[string]any) bool {
	switch strings.TrimSpace(anyString(card["status"])) {
	case "done", "cancelled":
		return false
	default:
		return true
	}
}

func boardCardRiskState(card map[string]any, now time.Time, riskHorizon time.Duration) (string, time.Time, bool) {
	if !boardCardCountsAsOpenWorkItem(card) {
		return "", time.Time{}, false
	}

	if strings.TrimSpace(anyString(card["column_key"])) == "blocked" {
		if dueAt, ok := parseOptionalRFC3339(anyString(card["due_at"])); ok && !dueAt.After(now.Add(riskHorizon)) {
			if dueAt.Before(now) {
				return "overdue", dueAt, true
			}
			return "blocked", dueAt, true
		}
		return "blocked", time.Time{}, false
	}

	dueAt, ok := parseOptionalRFC3339(anyString(card["due_at"]))
	if !ok {
		return "", time.Time{}, false
	}
	if dueAt.After(now.Add(riskHorizon)) {
		return "", time.Time{}, true
	}
	if dueAt.Before(now) {
		return "overdue", dueAt, true
	}
	return "due_soon", dueAt, true
}

func isSuppressedByAck(item derivedInboxItem, ackedAt map[string]time.Time) bool {
	acked, ok := ackedAt[item.ID]
	if !ok {
		return false
	}
	return !item.TriggerAt.After(acked)
}

func makeInboxItemID(category string, threadID string, subjectID string, sourceEventID string) string {
	if strings.TrimSpace(subjectID) == "" {
		subjectID = "none"
	}
	if strings.TrimSpace(sourceEventID) == "" {
		sourceEventID = "none"
	}
	return "inbox:" + category + ":" + threadID + ":" + subjectID + ":" + sourceEventID
}

func parseTimestamp(raw any) (time.Time, bool) {
	text, ok := raw.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func parseOptionalRFC3339(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err == nil {
		return parsed, true
	}
	parsed, err = time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func sortInboxItems(items []derivedInboxItem) {
	categoryOrder := map[string]int{
		"decision_needed":     0,
		"intervention_needed": 1,
		"exception":           2,
		"work_item_risk":      3,
	}

	sort.Slice(items, func(i int, j int) bool {
		left := items[i]
		right := items[j]

		leftOrder, ok := categoryOrder[left.Category]
		if !ok {
			leftOrder = 99
		}
		rightOrder, ok := categoryOrder[right.Category]
		if !ok {
			rightOrder = 99
		}
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}

		if left.Category == "work_item_risk" && right.Category == "work_item_risk" {
			if left.HasDueAt && right.HasDueAt && !left.DueAt.Equal(right.DueAt) {
				return left.DueAt.Before(right.DueAt)
			}
		}

		if !left.TriggerAt.Equal(right.TriggerAt) {
			return left.TriggerAt.After(right.TriggerAt)
		}

		return left.ID < right.ID
	})
}
