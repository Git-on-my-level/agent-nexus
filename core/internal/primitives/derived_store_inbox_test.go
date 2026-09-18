package primitives

import (
	"testing"
)

func TestDerivedInboxCategoryOrderRanksHumanAttentionKinds(t *testing.T) {
	t.Parallel()

	if got := derivedInboxCategoryOrder("escalate"); got != 0 {
		t.Fatalf("escalate order=%d want 0", got)
	}
	if got := derivedInboxCategoryOrder("ask"); got != 1 {
		t.Fatalf("ask order=%d want 1", got)
	}
	if got := derivedInboxCategoryOrder("review"); got != 2 {
		t.Fatalf("review order=%d want 2", got)
	}
	if got := derivedInboxCategoryOrder("risk_exception"); got != 99 {
		t.Fatalf("dead risk_exception category should sort last, got %d", got)
	}
}

func TestStripInboxDataForStoreOmitsColumnMirrors(t *testing.T) {
	t.Parallel()

	item := DerivedInboxItem{
		ID:            "inbox:ask:th:s:none:e1",
		ThreadID:      "th",
		Category:      "ask",
		TriggerAt:     "2026-01-01T00:00:00Z",
		DueAt:         "2026-01-10T00:00:00Z",
		HasDueAt:      true,
		SourceEventID: "e1",
		SourceCardID:  "",
		GeneratedAt:   "2026-01-02T00:00:00Z",
		Data: map[string]any{
			"id":                "stale",
			"thread_id":         "stale",
			"category":          "stale",
			"trigger_at":        "stale",
			"source_event_time": "stale",
			"source_event_id":   "stale",
			"due_at":            "stale",
			"generated_at":      "stale",
			"title":             "Hello",
		},
	}
	out := stripInboxDataForStore(item)
	if out["id"] != nil || out["thread_id"] != nil || out["category"] != nil ||
		out["trigger_at"] != nil || out["source_event_id"] != nil || out["due_at"] != nil ||
		out["source_event_time"] != nil || out["generated_at"] != nil {
		t.Fatalf("expected column-mirrored keys removed, got %v", out)
	}
	if out["title"] != "Hello" {
		t.Fatalf("expected extension field preserved, got %v", out)
	}
}

func TestRehydrateDerivedInboxDataFromColumnsRestoresFields(t *testing.T) {
	t.Parallel()

	item := DerivedInboxItem{
		ID:            "inbox:ask:th:s:none:e1",
		ThreadID:      "th",
		Category:      "ask",
		TriggerAt:     "2026-01-01T00:00:00Z",
		SourceEventID: "e1",
		GeneratedAt:   "2026-01-02T00:00:00Z",
		Data:          map[string]any{"title": "X"},
	}
	rehydrateDerivedInboxDataFromColumns(&item)
	if item.Data["id"] != item.ID || item.Data["thread_id"] != "th" || item.Data["kind"] != "ask" {
		t.Fatalf("unexpected rehydrate: %v", item.Data)
	}
	if item.Data["generated_at"] != "2026-01-02T00:00:00Z" {
		t.Fatalf("expected generated_at from column, got %v", item.Data["generated_at"])
	}
}
