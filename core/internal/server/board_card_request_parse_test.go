package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestParseAddBoardCardJSONRejectsMixedAliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  map[string]any
	}{
		{
			name: "summary with legacy body",
			raw: map[string]any{
				"title":   "Card title",
				"summary": "Canonical summary",
				"body":    "Legacy body",
			},
		},
		{
			name: "legacy body markdown",
			raw: map[string]any{
				"title":         "Card title",
				"body_markdown": "Legacy body",
			},
		},
		{
			name: "assignee refs with legacy assignee",
			raw: map[string]any{
				"title":         "Card title",
				"summary":       "Canonical summary",
				"assignee_refs": []any{"actor:alice"},
				"assignee":      "alice",
			},
		},
		{
			name: "legacy priority",
			raw: map[string]any{
				"title":    "Card title",
				"summary":  "Canonical summary",
				"priority": "high",
			},
		},
		{
			name: "legacy status only",
			raw: map[string]any{
				"title":   "Card title",
				"summary": "Canonical summary",
				"status":  "todo",
			},
		},
		{
			name: "document ref with pinned document id",
			raw: map[string]any{
				"title":              "Card title",
				"summary":            "Canonical summary",
				"document_ref":       "document:doc-1",
				"pinned_document_id": "doc-1",
			},
		},
		{
			name: "legacy pinned document id only",
			raw: map[string]any{
				"title":              "Card title",
				"summary":            "Canonical summary",
				"pinned_document_id": "doc-1",
			},
		},
		{
			name: "related refs with refs",
			raw: map[string]any{
				"title":        "Card title",
				"summary":      "Canonical summary",
				"related_refs": []any{"thread:thr-1"},
				"refs":         []any{"topic:top-1"},
			},
		},
		{
			name: "nested card envelope aliases",
			raw: map[string]any{
				"card": map[string]any{
					"title":        "Card title",
					"summary":      "Canonical summary",
					"related_refs": []any{"thread:thr-1"},
					"refs":         []any{"topic:top-1"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			_, ok := parseAddBoardCardJSON(rec, tt.raw)
			if ok {
				t.Fatal("expected parse failure")
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status: got %d want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestParseAddBoardCardJSONAcceptsCanonicalShape(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	merged, ok := parseAddBoardCardJSON(rec, map[string]any{
		"actor_id": "actor-1",
		"card": map[string]any{
			"title":              "Card title",
			"summary":            "Canonical summary",
			"column_key":         "ready",
			"assignee_refs":      []any{"actor:alice"},
			"document_ref":       "document:doc-1",
			"related_refs":       []any{"thread:thr-1"},
			"definition_of_done": []any{"receipt"},
			"risk":               "medium",
		},
	})
	if !ok {
		t.Fatalf("expected parse success, got status=%d body=%s", rec.Code, rec.Body.String())
	}
	if merged.Title != "Card title" || merged.Body != "Canonical summary" {
		t.Fatalf("unexpected parsed title/body: %#v", merged)
	}
	if merged.Assignee == nil || *merged.Assignee != "alice" {
		t.Fatalf("expected assignee storage string alice, got %#v", merged.Assignee)
	}
	if merged.PinnedDocumentID == nil || *merged.PinnedDocumentID != "doc-1" {
		t.Fatalf("expected pinned document id doc-1, got %#v", merged.PinnedDocumentID)
	}
	if len(merged.Refs) != 1 || merged.Refs[0] != "thread:thr-1" {
		t.Fatalf("unexpected refs: %#v", merged.Refs)
	}
}
