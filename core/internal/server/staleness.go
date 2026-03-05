package server

import (
	"context"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schedule"
)

const customCadenceWindow = 7 * 24 * time.Hour

func emitStaleThreadExceptions(ctx context.Context, opts handlerOptions, now time.Time, actorID string) error {
	if opts.primitiveStore == nil {
		return nil
	}

	threads, err := opts.primitiveStore.ListThreads(ctx, primitives.ThreadListFilter{})
	if err != nil {
		return err
	}

	events, err := opts.primitiveStore.ListEvents(ctx, primitives.EventListFilter{
		Types: []string{"receipt_added", "decision_made", "exception_raised"},
	})
	if err != nil {
		return err
	}

	latestActivity := latestThreadActivityFromEvents(events)
	latestStaleException := latestStaleExceptionByThread(events)

	actor := strings.TrimSpace(actorID)
	if actor == "" {
		actor = "oar-core"
	}

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
			"refs":      []string{"snapshot:" + threadID},
			"summary":   "thread is stale",
			"payload": map[string]any{
				"subtype": "stale_thread",
			},
			"provenance": map[string]any{"sources": []string{"inferred"}},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func latestThreadActivityFromEvents(events []map[string]any) map[string]time.Time {
	out := make(map[string]time.Time)
	for _, event := range events {
		eventType, _ := event["type"].(string)
		if eventType != "receipt_added" && eventType != "decision_made" {
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

func latestStaleExceptionByThread(events []map[string]any) map[string]time.Time {
	out := make(map[string]time.Time)
	for _, event := range events {
		eventType, _ := event["type"].(string)
		if eventType != "exception_raised" {
			continue
		}
		payload, _ := event["payload"].(map[string]any)
		subtype, _ := payload["subtype"].(string)
		if subtype != "stale_thread" {
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
