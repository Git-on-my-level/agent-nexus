package server

import (
	"strings"
	"testing"
)

func TestCardDisplayNamePrefersTitleOverThreadID(t *testing.T) {
	t.Parallel()

	card := map[string]any{
		"id":        "card-uuid-1",
		"thread_id": "thread-uuid-1",
		"title":     "Supply Chain Response",
		"summary":   "fallback summary",
	}
	if got := cardDisplayName(card); got != "Supply Chain Response" {
		t.Fatalf("cardDisplayName = %q, want title", got)
	}
}

func TestCardDisplayNameFallsBackToSummaryThenID(t *testing.T) {
	t.Parallel()

	c1 := map[string]any{
		"id":        "card-uuid-1",
		"thread_id": "thread-uuid-1",
		"summary":   "Only summary",
	}
	if got := cardDisplayName(c1); got != "Only summary" {
		t.Fatalf("cardDisplayName = %q", got)
	}
	c2 := map[string]any{"id": "cid-2", "thread_id": "tid-2"}
	if got := cardDisplayName(c2); got != "cid-2" {
		t.Fatalf("cardDisplayName = %q", got)
	}
}

func TestCardTimelineEventMatches(t *testing.T) {
	t.Parallel()

	cardID := "card-abc"
	yes := map[string]any{
		"type": "card_updated",
		"refs": []any{"board:b1", "card:card-abc", "thread:t1"},
		"payload": map[string]any{
			"board_id": "b1",
			"card_id":  "card-abc",
		},
	}
	if !cardTimelineEventMatches(yes, cardID) {
		t.Fatal("expected match via refs")
	}

	payloadOnly := map[string]any{
		"type":    "card_updated",
		"refs":    []any{"board:b1"},
		"payload": map[string]any{"card_id": "card-abc"},
	}
	if !cardTimelineEventMatches(payloadOnly, cardID) {
		t.Fatal("expected match via payload.card_id")
	}

	// Intentional split: the card timeline is the lifecycle/audit log. Card
	// Discussion messages live on the backing thread and are served by
	// GET /threads/{thread_id}/timeline, so message_posted must not match here
	// even when it carries a card:<id> ref. See cardTimelineEventMatches.
	msg := map[string]any{
		"type": "message_posted",
		"refs": []any{"thread:t1"},
	}
	if cardTimelineEventMatches(msg, cardID) {
		t.Fatal("message should not match card filter")
	}

	msgWithCardRef := map[string]any{
		"type": "message_posted",
		"refs": []any{"thread:t1", "card:card-abc"},
	}
	if cardTimelineEventMatches(msgWithCardRef, cardID) {
		t.Fatal("message with card ref should not match card lifecycle filter")
	}
}

func TestBuildCardCreatedEventIncludesRevisionBackedFields(t *testing.T) {
	t.Parallel()

	board := map[string]any{"id": "board-1", "thread_id": "t-board", "refs": []any{}}
	card := map[string]any{
		"id":                 "c1",
		"thread_id":          "ct",
		"title":              "T1",
		"summary":            "Body",
		"column_key":         "ready",
		"assignee_refs":      []any{"actor:alice"},
		"related_refs":       []any{"thread:thr-1"},
		"definition_of_done": []any{"one", "two"},
		"resolution_refs":    []any{},
		"risk":               "high",
		"due_at":             "2026-04-01T00:00:00Z",
	}
	ev := buildCardCreatedEvent(board, card)
	summary := strings.TrimSpace(asString(ev["summary"]))
	if !strings.Contains(summary, "T1") {
		t.Fatalf("summary should include title, got %q", summary)
	}
	payload, ok := ev["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload: %#v", ev["payload"])
	}
	if asString(payload["title"]) != "T1" {
		t.Fatalf("title field: %#v", payload["title"])
	}
	if asString(payload["summary"]) != "Body" {
		t.Fatalf("summary field: %#v", payload["summary"])
	}
}
