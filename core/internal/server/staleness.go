package server

import (
	"context"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schedule"
)

const customCadenceWindow = 7 * 24 * time.Hour

func emitStaleThreadExceptions(ctx context.Context, opts handlerOptions, now time.Time, actorID string) ([]string, error) {
	if opts.primitiveStore == nil {
		return nil, nil
	}

	threads, _, err := opts.primitiveStore.ListThreads(ctx, primitives.ThreadListFilter{})
	if err != nil {
		return nil, err
	}

	events, err := opts.primitiveStore.ListEvents(ctx, primitives.EventListFilter{})
	if err != nil {
		return nil, err
	}
	cards, err := opts.primitiveStore.ListCards(ctx, primitives.CardListFilter{})
	if err != nil {
		return nil, err
	}

	latestActivity := mergeThreadActivity(latestThreadActivityFromEvents(events), latestThreadActivityFromCards(cards))
	latestStaleException := latestStaleExceptionByThread(events)

	actor := strings.TrimSpace(actorID)
	if actor == "" {
		actor = actors.SystemActorID
	}
	emittedThreadIDs := make([]string, 0)
	for _, thread := range threads {
		threadID, _ := thread["id"].(string)
		if strings.TrimSpace(threadID) == "" {
			continue
		}

		activityAt := latestActivity[threadID]
		if !isThreadStaleAt(now, thread, activityAt) {
			continue
		}

		lastStale := latestStaleException[threadID]
		if !lastStale.IsZero() && (activityAt.IsZero() || !activityAt.After(lastStale)) {
			continue
		}

		_, err := opts.primitiveStore.AppendEvent(ctx, actor, map[string]any{
			"type":      "exception_raised",
			"thread_id": threadID,
			"refs":      []string{"thread:" + threadID},
			"summary":   "thread is stale",
			"payload": map[string]any{
				"subtype": "stale_topic",
			},
			"provenance": map[string]any{"sources": []string{"inferred"}},
		})
		if err != nil {
			return nil, err
		}
		emittedThreadIDs = append(emittedThreadIDs, threadID)
	}

	return uniqueServerStrings(emittedThreadIDs), nil
}

func latestThreadActivityFromEvents(events []map[string]any) map[string]time.Time {
	out := make(map[string]time.Time)
	for _, event := range events {
		if !isMeaningfulThreadActivityEvent(event) {
			continue
		}
		threadID, _ := event["thread_id"].(string)
		if strings.TrimSpace(threadID) == "" {
			continue
		}
		ts, ok := parseTimestamp(event["ts"])
		if !ok {
			continue
		}
		if current, exists := out[threadID]; !exists || ts.After(current) {
			out[threadID] = ts
		}
	}
	return out
}

func latestThreadActivityFromCards(cards []map[string]any) map[string]time.Time {
	out := make(map[string]time.Time)
	for _, card := range cards {
		threadID := strings.TrimSpace(primaryRelatedThreadID(card))
		if threadID == "" {
			threadID = strings.TrimSpace(anyString(card["thread_id"]))
		}
		if threadID == "" {
			continue
		}
		updatedAt, ok := parseTimestamp(card["updated_at"])
		if !ok {
			continue
		}
		if current, exists := out[threadID]; !exists || updatedAt.After(current) {
			out[threadID] = updatedAt
		}
	}
	return out
}

func mergeThreadActivity(activitySets ...map[string]time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	for _, activitySet := range activitySets {
		for threadID, ts := range activitySet {
			if current, exists := out[threadID]; !exists || ts.After(current) {
				out[threadID] = ts
			}
		}
	}
	return out
}

func isMeaningfulThreadActivityEvent(event map[string]any) bool {
	eventType, _ := event["type"].(string)
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return false
	}

	switch eventType {
	case "actor_statement",
		"decision_needed",
		"intervention_needed",
		"decision_made",
		"card_created",
		"card_updated",
		"card_moved",
		"card_archived",
		"receipt_added",
		"review_completed",
		"document_created",
		"document_updated",
		"document_trashed",
		"document_restored":
		return true
	case "inbox_item_acknowledged", "exception_raised":
		return false
	case "thread_created":
		return false
	case "thread_updated":
		payload, _ := event["payload"].(map[string]any)
		changedFields, err := extractStringSlice(payload["changed_fields"])
		if err == nil && len(changedFields) == 1 && strings.TrimSpace(changedFields[0]) == "open_cards" {
			return false
		}
		return true
	default:
		return false
	}
}

func latestStaleExceptionByThread(events []map[string]any) map[string]time.Time {
	out := make(map[string]time.Time)
	for _, event := range events {
		eventType, _ := event["type"].(string)
		if eventType != "exception_raised" {
			continue
		}
		payload, _ := event["payload"].(map[string]any)
		subtype, _ := payload["subtype"].(string)
		if subtype != "stale_topic" {
			continue
		}
		threadID, _ := event["thread_id"].(string)
		if strings.TrimSpace(threadID) == "" {
			continue
		}
		ts, ok := parseTimestamp(event["ts"])
		if !ok {
			continue
		}
		if current, exists := out[threadID]; !exists || ts.After(current) {
			out[threadID] = ts
		}
	}
	return out
}

func stalenessByThread(threads []map[string]any, events []map[string]any, now time.Time) map[string]bool {
	activityByThread := latestThreadActivityFromEvents(events)
	out := make(map[string]bool, len(threads))
	for _, thread := range threads {
		threadID, _ := thread["id"].(string)
		if strings.TrimSpace(threadID) == "" {
			continue
		}
		out[threadID] = isThreadStaleAt(now, thread, activityByThread[threadID])
	}
	return out
}

func isThreadStaleAt(now time.Time, thread map[string]any, lastActivityAt time.Time) bool {
	cadence, _ := thread["cadence"].(string)
	cadence = schedule.NormalizeCadence(cadence)
	if schedule.IsReactiveCadence(cadence) {
		return false
	}

	nextCheckInText, _ := thread["next_check_in_at"].(string)
	nextCheckInAt, err := time.Parse(time.RFC3339, strings.TrimSpace(nextCheckInText))
	if err != nil {
		return false
	}
	if !now.After(nextCheckInAt) {
		return false
	}

	windowStart, ok := cadenceWindowStart(cadence, now, nextCheckInAt)
	if !ok {
		return false
	}

	if lastActivityAt.IsZero() {
		return true
	}

	return lastActivityAt.Before(windowStart)
}

func cadenceWindowStart(cadence string, now time.Time, nextCheckInAt time.Time) (time.Time, bool) {
	switch cadence {
	case "daily":
		return now.Add(-24 * time.Hour), true
	case "weekly":
		return now.Add(-7 * 24 * time.Hour), true
	case "monthly":
		return now.Add(-30 * 24 * time.Hour), true
	case "custom":
		// Implementation-defined: custom cadence uses a 7-day lookback window anchored to next_check_in_at.
		return nextCheckInAt.Add(-customCadenceWindow), true
	default:
		previousRun, ok := schedule.PreviousCronRun(cadence, now)
		if !ok {
			return time.Time{}, false
		}
		return previousRun, true
	}
}
