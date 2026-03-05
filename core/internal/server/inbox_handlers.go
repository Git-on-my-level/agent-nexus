package server

import (
	"context"
	"encoding/json"
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

func handleGetInbox(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	now := time.Now().UTC()
	horizon := opts.inboxRiskHorizon
	if horizon <= 0 {
		horizon = defaultInboxRiskHorizon
	}

	if rawDays := strings.TrimSpace(r.URL.Query().Get("risk_horizon_days")); rawDays != "" {
		days, err := strconv.Atoi(rawDays)
		if err != nil || days < 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "risk_horizon_days must be a non-negative integer")
			return
		}
		horizon = time.Duration(days) * 24 * time.Hour
	}

	items, err := deriveInboxItems(r.Context(), opts, now, horizon)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to derive inbox items")
		return
	}

	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payloadItems = append(payloadItems, item.Data)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":        payloadItems,
		"generated_at": now.Format(time.RFC3339Nano),
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	actorID, ok := requireRegisteredActorID(w, r, opts.actorRegistry, req.ActorID)
	if !ok {
		return
	}

	now := time.Now().UTC()
	if err := emitStaleThreadExceptions(r.Context(), opts, now, actorID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to rebuild derived views")
		return
	}
	horizon := opts.inboxRiskHorizon
	if horizon <= 0 {
		horizon = defaultInboxRiskHorizon
	}
	if _, err := deriveInboxItemsNoStaleEmission(r.Context(), opts, now, horizon); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	actorID, ok := requireRegisteredActorID(w, r, opts.actorRegistry, req.ActorID)
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

	writeJSON(w, http.StatusCreated, map[string]any{"event": stored})
}

func deriveInboxItems(ctx context.Context, opts handlerOptions, now time.Time, riskHorizon time.Duration) ([]derivedInboxItem, error) {
	if err := emitStaleThreadExceptions(ctx, opts, now, ""); err != nil {
		return nil, err
	}
	return deriveInboxItemsNoStaleEmission(ctx, opts, now, riskHorizon)
}

func deriveInboxItemsNoStaleEmission(ctx context.Context, opts handlerOptions, now time.Time, riskHorizon time.Duration) ([]derivedInboxItem, error) {
	events, err := opts.primitiveStore.ListEvents(ctx, primitives.EventListFilter{
		Types: []string{"decision_needed", "exception_raised", "inbox_item_acknowledged", "receipt_added", "decision_made"},
	})
	if err != nil {
		return nil, err
	}

	ackedAt := latestInboxAcknowledgments(events)
	latestActivity := latestThreadActivityFromEvents(events)
	items := make([]derivedInboxItem, 0)

	for _, event := range events {
		eventType, _ := event["type"].(string)
		switch eventType {
		case "decision_needed", "exception_raised":
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
			items = append(items, item)
		}
	}

	commitments, err := opts.primitiveStore.ListCommitments(ctx, primitives.CommitmentListFilter{})
	if err != nil {
		return nil, err
	}

	for _, commitment := range commitments {
		item, ok := deriveCommitmentRiskInboxItem(commitment, now, riskHorizon)
		if !ok {
			continue
		}
		if isSuppressedByAck(item, ackedAt) {
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

func deriveCommitmentRiskInboxItem(commitment map[string]any, now time.Time, riskHorizon time.Duration) (derivedInboxItem, bool) {
	status, _ := commitment["status"].(string)
	if status != "open" && status != "blocked" {
		return derivedInboxItem{}, false
	}

	dueAtText, _ := commitment["due_at"].(string)
	dueAt, err := time.Parse(time.RFC3339, strings.TrimSpace(dueAtText))
	if err != nil {
		return derivedInboxItem{}, false
	}
	if dueAt.After(now.Add(riskHorizon)) {
		return derivedInboxItem{}, false
	}

	threadID, _ := commitment["thread_id"].(string)
	commitmentID, _ := commitment["id"].(string)
	if strings.TrimSpace(threadID) == "" || strings.TrimSpace(commitmentID) == "" {
		return derivedInboxItem{}, false
	}

	triggerAt, ok := parseTimestamp(commitment["updated_at"])
	if !ok {
		triggerAt = now
	}

	title, _ := commitment["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Commitment risk"
	}

	recommendedAction := "follow_up_commitment"
	if dueAt.Before(now) {
		recommendedAction = "resolve_overdue_commitment"
	}

	id := makeInboxItemID("commitment_risk", threadID, commitmentID, "")
	data := map[string]any{
		"id":                 id,
		"category":           "commitment_risk",
		"thread_id":          threadID,
		"commitment_id":      commitmentID,
		"title":              title,
		"recommended_action": recommendedAction,
		"due_at":             dueAt.Format(time.RFC3339),
	}

	return derivedInboxItem{
		Data:      data,
		Category:  "commitment_risk",
		ID:        id,
		TriggerAt: triggerAt,
		DueAt:     dueAt,
		HasDueAt:  true,
	}, true
}

func isSuppressedByAck(item derivedInboxItem, ackedAt map[string]time.Time) bool {
	acked, ok := ackedAt[item.ID]
	if !ok {
		return false
	}
	return !item.TriggerAt.After(acked)
}

func makeInboxItemID(category string, threadID string, commitmentID string, sourceEventID string) string {
	if strings.TrimSpace(commitmentID) == "" {
		commitmentID = "none"
	}
	if strings.TrimSpace(sourceEventID) == "" {
		sourceEventID = "none"
	}
	return "inbox:" + category + ":" + threadID + ":" + commitmentID + ":" + sourceEventID
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

func sortInboxItems(items []derivedInboxItem) {
	categoryOrder := map[string]int{
		"decision_needed": 0,
		"exception":       1,
		"commitment_risk": 2,
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

		if left.Category == "commitment_risk" && right.Category == "commitment_risk" {
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
