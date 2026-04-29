package server

import (
	"context"
	"strings"
	"time"
)

func emitStaleThreadExceptions(ctx context.Context, opts handlerOptions, now time.Time, actorID string) ([]string, error) {
	if opts.primitiveStore == nil {
		return nil, nil
	}
	_ = ctx
	_ = opts
	_ = now
	_ = actorID
	// Dumb-thread model: do not infer staleness from cadence / next_check_in_at in thread JSON.
	// Reminders belong in a future automation policy resource, not backing-thread bodies.
	return nil, nil
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

func isMeaningfulThreadActivityEvent(event map[string]any) bool {
	eventType, _ := event["type"].(string)
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return false
	}

	switch eventType {
	case "topic_archived",
		"topic_restored",
		"topic_trashed",
		"card_created",
		"card_updated",
		"card_moved",
		"card_archived",
		"card_trashed",
		"card_resolved",
		"document_created",
		"document_revised",
		"document_trashed",
		"document_restored":
		return true
	case "human_attention_requested", "human_attention_responded", "exception_raised":
		return false
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

// isThreadStaleAt is always false: launch threads do not carry schedule/cadence semantics in JSON.
func isThreadStaleAt(now time.Time, thread map[string]any, lastActivityAt time.Time) bool {
	_ = now
	_ = thread
	_ = lastActivityAt
	return false
}
