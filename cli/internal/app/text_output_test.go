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

func TestFormatBoardCardCreateResult_IsConciseAndShowsCardRef(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"board": map[string]any{"updated_at": "2026-03-08T00:00:00Z"},
		"card": map[string]any{
			"id":         "card_1234567890abcdef",
			"ref":        "card:fix-cli-card-discovery",
			"handle":     "fix-cli-card-discovery",
			"thread_id":  "thread_1234567890abcdef",
			"thread_ref": "thread:cli-card-discovery",
			"title":      "Fix CLI card discovery",
			"column_key": "backlog",
			"rank":       "a",
		},
	}
	got := formatCommandSummary("cards.create", body)
	if strings.Contains(got, `"card"`) || strings.Contains(got, "thread_1234567890abcdef") || strings.Contains(got, "card_12345") {
		t.Fatalf("expected concise text create output, got:\n%s", got)
	}
	if !strings.Contains(got, "Card created:") || !strings.Contains(got, "- card: card:fix-cli-card-discovery — Fix CLI card discovery") || !strings.Contains(got, "thread: thread:cli-card-discovery") {
		t.Fatalf("expected card ref create output, got:\n%s", got)
	}
}

func TestFormatBoardCardsList_LeadsWithCardRefAndTitle(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"board_id": "board_1234567890abcdef",
		"cards": []any{
			map[string]any{
				"id":         "card_1234567890abcdef",
				"ref":        "card:fix-cli-card-discovery",
				"thread_id":  "thread_1234567890abcdef",
				"thread_ref": "thread:cli-card-discovery",
				"title":      "Fix CLI card discovery",
				"column_key": "backlog",
				"rank":       "a",
			},
		},
	}
	got := formatBoardCardsList(body)
	want := "- card:fix-cli-card-discovery :: backlog :: Fix CLI card discovery :: thread=thread:cli-card-discovery :: rank=a"
	if !strings.Contains(got, want) {
		t.Fatalf("expected card ref and title before thread ref, got:\n%s", got)
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
	if !strings.Contains(got, "- card_1234567890abcdef :: backlog :: Fix CLI card discovery :: thread=thread:thread_1234567890abcdef") {
		t.Fatalf("expected full canonical card id in full-id mode, got:\n%s", got)
	}
}

func TestFormatBoardWorkspaceRendersNestedBackingThreadCards(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"board": map[string]any{
			"id":    "board_1234567890abcdef",
			"title": "Ops Board",
			"state": "active",
		},
		"cards": map[string]any{
			"items": []any{
				map[string]any{
					"card": map[string]any{
						"id":         "card_1234567890abcdef",
						"ref":        "card:fix-cli-card-discovery",
						"title":      "Fix CLI card discovery",
						"column_key": "backlog",
					},
					"backing": map[string]any{
						"thread": map[string]any{
							"id":    "thread_1234567890abcdef",
							"title": "Fix CLI card discovery",
						},
					},
				},
			},
		},
	}
	got := formatBoardWorkspace(body)
	if strings.Contains(got, "- (empty)") {
		t.Fatalf("expected card title instead of empty row, got:\n%s", got)
	}
	if !strings.Contains(got, "- card:fix-cli-card-discovery :: Fix CLI card discovery :: thread=thread_1234567890abcdef") {
		t.Fatalf("expected nested backing thread card row led by card ref, got:\n%s", got)
	}
}

func TestFormatBoardCardRemoveResult_WithCardStandalone(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"board": map[string]any{"updated_at": "2026-03-08T00:00:00Z"},
		"card": map[string]any{
			"id":         "card_xyz789",
			"ref":        "card:standalone-task",
			"handle":     "standalone-task",
			"title":      "Standalone task",
			"column_key": "backlog",
			"rank":       "a",
		},
	}
	got := formatBoardCardRemoveResult(body)
	if !strings.Contains(got, "Card removed:") {
		t.Fatalf("expected headline, got %q", got)
	}
	if strings.Contains(got, "card_xyz789") {
		t.Fatalf("expected public card ref instead of internal id, got %q", got)
	}
	if !strings.Contains(got, "- card: card:standalone-task — Standalone task") {
		t.Fatalf("expected card line with ref and title, got %q", got)
	}
}

func TestFormatBoardCardsBatchCreateResult_UsesCardRefs(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"board": map[string]any{"updated_at": "2026-03-08T00:00:00Z"},
		"cards": []any{
			map[string]any{
				"id":     "card_internal_1234567890",
				"ref":    "card:first-task",
				"handle": "first-task",
				"title":  "First task",
			},
		},
	}
	got := formatBoardCardsBatchCreateResult(body)
	if strings.Contains(got, "card_internal_1234567890") {
		t.Fatalf("expected batch create text to hide internal id, got:\n%s", got)
	}
	if !strings.Contains(got, "- [0] card:first-task — First task") {
		t.Fatalf("expected batch create text to use card ref, got:\n%s", got)
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
