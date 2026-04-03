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
