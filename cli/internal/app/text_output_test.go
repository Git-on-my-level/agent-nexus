package app

import (
	"strings"
	"testing"
)

func TestFormatBoardCardRemoveResult_WithCardThreadBacked(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"board": map[string]any{"updated_at": "2026-03-08T00:00:00Z"},
		"card": map[string]any{
			"thread_id":  "thread_abc123",
			"column_key": "ready",
			"rank":       "m",
		},
	}
	got := formatBoardCardRemoveResult(body)
	if !strings.Contains(got, "Card removed:") {
		t.Fatalf("expected headline, got %q", got)
	}
	if !strings.Contains(got, "- thread: thread_abc123") {
		t.Fatalf("expected thread line, got %q", got)
	}
	if !strings.Contains(got, "column: ready") {
		t.Fatalf("expected column, got %q", got)
	}
}

func TestFormatBoardCardsList_LeadsWithCanonicalCardShortIDAndTitle(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"board_id": "board_1234567890abcdef",
		"cards": []any{
			map[string]any{
				"id":         "card_1234567890abcdef",
				"short_id":   "card_12345",
				"thread_id":  "thread_1234567890abcdef",
				"title":      "Fix CLI card discovery",
				"column_key": "backlog",
				"rank":       "a",
			},
		},
	}
	got := formatBoardCardsList(body)
	want := "- card_12345 :: backlog :: Fix CLI card discovery :: thread=thread_1234567890abcdef :: rank=a"
	if !strings.Contains(got, want) {
		t.Fatalf("expected canonical card id and title before thread id, got:\n%s", got)
	}
}

func TestFormatBoardCardsList_FullIDUsesCanonicalCardID(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"full_id": true,
		"cards": []any{
			map[string]any{
				"id":         "card_1234567890abcdef",
				"short_id":   "card_12345",
				"thread_id":  "thread_1234567890abcdef",
				"title":      "Fix CLI card discovery",
				"column_key": "backlog",
			},
		},
	}
	got := formatBoardCardsList(body)
	if !strings.Contains(got, "- card_1234567890abcdef :: backlog :: Fix CLI card discovery :: thread=thread_1234567890abcdef") {
		t.Fatalf("expected full canonical card id in full-id mode, got:\n%s", got)
	}
}

func TestFormatBoardCardRemoveResult_WithCardStandalone(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"board": map[string]any{"updated_at": "2026-03-08T00:00:00Z"},
		"card": map[string]any{
			"id":         "card_xyz789",
			"title":      "Standalone task",
			"column_key": "backlog",
			"rank":       "a",
		},
	}
	got := formatBoardCardRemoveResult(body)
	if !strings.Contains(got, "Card removed:") {
		t.Fatalf("expected headline, got %q", got)
	}
	if !strings.Contains(got, "- card: card_xyz789 — Standalone task") {
		t.Fatalf("expected card line with id and title, got %q", got)
	}
}

func TestFormatCardRecord_Trashed(t *testing.T) {
	t.Parallel()
	card := map[string]any{
		"id":           "card_abc",
		"short_id":     "c1",
		"trashed_at":   "2026-01-01T00:00:00Z",
		"trashed_by":   "actor_1",
		"trash_reason": "cleanup",
	}
	got := formatCardRecord(card)
	if !strings.Contains(got, "⚠ TRASHED") {
		t.Fatalf("expected TRASHED banner, got %q", got)
	}
	if !strings.Contains(got, "trashed_at:") {
		t.Fatalf("expected trashed_at, got %q", got)
	}
}

func TestFormatBoardsList_TextScanStripsBoardPrefix(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"boards": []any{
			map[string]any{
				"board": map[string]any{
					"id":     "board-summer-menu-plan",
					"title":  "Summer menu",
					"status": "active",
				},
				"summary": map[string]any{"card_count": 2, "unresolved_card_count": 1, "document_count": 0},
			},
		},
	}
	got := formatBoardsList(body)
	// Tail after "board-" is summer-menu-plan; first shortIDLength (10) runes: summer-men
	if !strings.Contains(got, "summer-men") || strings.Contains(got, "board-su") {
		t.Fatalf("expected scan-style id tail (10 runes) without redundant board- prefix, got:\n%s", got)
	}
}

func TestRenderBoardCardItem_UsesRiskExceptionBadge(t *testing.T) {
	t.Parallel()
	cardWrapper := map[string]any{
		"thread": map[string]any{
			"id":    "thread_123",
			"title": "Machine-facing consistency",
		},
		"summary": map[string]any{
			"related_topic_count":    1,
			"decision_request_count": 1,
			"decision_count":         0,
			"recommendation_count":   1,
			"document_count":         1,
			"inbox_count":            1,
			"stale":                  true,
		},
	}
	got := renderBoardCardItem(cardWrapper)
	if strings.Contains(got, "STALE") {
		t.Fatalf("expected stale badge to be renamed, got %q", got)
	}
	if !strings.Contains(got, "risk_exception") {
		t.Fatalf("expected risk_exception badge, got %q", got)
	}
}

func TestFormatNamedList_ThreadsScanStripsThreadPrefix(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"threads": []any{
			map[string]any{
				"id":     "thread-kids-lemonade-main",
				"title":  "Lemonade plan",
				"status": "active",
			},
		},
	}
	got := formatNamedList(body, "threads", "Threads", "thread", renderThreadListItem)
	// Tail after "thread-" is kids-lemonade-main; first shortIDLength (10) runes: kids-lemon
	if !strings.Contains(got, "kids-lemon") || strings.Contains(got, "thread-k") {
		t.Fatalf("expected scan-style id (10 runes) after thread- prefix, got:\n%s", got)
	}
}

func TestDisambiguateListScanIDs_AppendsShortWhenCollision(t *testing.T) {
	t.Parallel()
	items := []map[string]any{
		{"board": map[string]any{"id": "board-summer-menu-a", "title": "A"}},
		{"board": map[string]any{"id": "board-summer-menu-b", "title": "B"}},
	}
	got := disambiguateListScanIDs(items, "board", false)
	if len(got) != 2 {
		t.Fatalf("expected 2 labels, got %#v", got)
	}
	// Both tails share the same first 10 runes (summer-men), so labels must disambiguate with brackets.
	for i, label := range got {
		if !strings.HasPrefix(label, "summer-men [") || !strings.Contains(label, "]") {
			t.Fatalf("expected disambiguated 10-rune scan prefix with bracket suffix, got[%d]=%q", i, label)
		}
	}
}

func TestFormatBoardCardRemoveResult_LegacyRemovedThreadOnly(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"board":             map[string]any{"updated_at": "2026-03-08T00:00:00Z"},
		"removed_thread_id": "thread_legacy",
	}
	got := formatBoardCardRemoveResult(body)
	if !strings.Contains(got, "Card removed:") {
		t.Fatalf("expected headline, got %q", got)
	}
	if !strings.Contains(got, "- thread: thread_legacy") {
		t.Fatalf("expected legacy thread line, got %q", got)
	}
}
