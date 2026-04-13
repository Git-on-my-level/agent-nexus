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
