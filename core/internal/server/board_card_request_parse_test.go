package server

import "testing"

func TestFlattenLegacyMoveCardEnvelope(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"actor_id": "actor-1",
		"move": map[string]any{
			"column_key":          "blocked",
			"if_board_updated_at": "2026-01-01T00:00:00.000Z",
			"before_card_id":      "card-anchor",
		},
	}
	flattenLegacyMoveCardEnvelope(raw)
	if _, ok := raw["move"]; ok {
		t.Fatal("expected legacy move envelope removed")
	}
	if got := anyString(raw["column_key"]); got != "blocked" {
		t.Fatalf("column_key: got %q want blocked", got)
	}
	if got := anyString(raw["if_board_updated_at"]); got != "2026-01-01T00:00:00.000Z" {
		t.Fatalf("if_board_updated_at: got %q", got)
	}
	if got := anyString(raw["before_card_id"]); got != "card-anchor" {
		t.Fatalf("before_card_id: got %q", got)
	}

	rootWins := map[string]any{
		"column_key": "ready",
		"move": map[string]any{
			"column_key": "blocked",
		},
	}
	flattenLegacyMoveCardEnvelope(rootWins)
	if got := anyString(rootWins["column_key"]); got != "ready" {
		t.Fatalf("root column_key must win, got %q", got)
	}
	if _, ok := rootWins["move"]; ok {
		t.Fatal("expected move key stripped when root column_key set")
	}
}
