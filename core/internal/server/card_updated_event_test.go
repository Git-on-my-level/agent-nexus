package server

import (
	"reflect"
	"testing"
)

func TestBuildCardLifecycleEventsPreferCanonicalFields(t *testing.T) {
	t.Parallel()

	board := map[string]any{"id": "board-1", "thread_id": "t-board", "refs": []any{}}
	card := map[string]any{
		"id":            "c1",
		"thread_id":     "card-thread",
		"column_key":    "ready",
		"status":        "todo",
		"assignee_refs": []any{"actor:alice"},
		"document_ref":  "document:doc-1",
		"related_refs":  []any{"thread:thr-1", "topic:t1"},
	}

	for _, ev := range []map[string]any{
		buildBoardCardAddedEvent(board, card),
		buildCardCreatedEvent(board, card),
		buildCardArchivedEvent(board, card),
		buildCardTrashedEvent(board, card),
		buildBoardCardArchivedEvent(board, card),
		buildBoardCardTrashedEvent(board, card),
	} {
		payload, ok := ev["payload"].(map[string]any)
		if !ok {
			t.Fatalf("expected payload map, got %#v", ev["payload"])
		}
		if !reflect.DeepEqual(payload["related_refs"], []any{"thread:thr-1", "topic:t1"}) {
			t.Fatalf("related_refs: %#v", payload["related_refs"])
		}
		if payload["document_ref"] != "document:doc-1" {
			t.Fatalf("document_ref: %#v", payload["document_ref"])
		}
		if _, ok := payload["parent_thread"]; ok {
			t.Fatalf("expected payload to omit parent_thread, got %#v", payload)
		}
		if _, ok := payload["pinned_document_id"]; ok {
			t.Fatalf("expected payload to omit pinned_document_id, got %#v", payload)
		}
	}
}

func TestBuildCardUpdatedEventAssigneeRefsAndRelatedRefs(t *testing.T) {
	t.Parallel()

	board := map[string]any{"id": "board-1", "thread_id": "t-board", "refs": []any{}}
	prev := map[string]any{
		"id":            "c1",
		"thread_id":     "ct",
		"assignee_refs": []any{"actor:alice"},
		"related_refs":  []any{"thread:thr-1", "topic:t1"},
	}
	upd := map[string]any{
		"id":            "c1",
		"thread_id":     "ct",
		"assignee_refs": []any{"actor:bob"},
		"related_refs":  []any{"thread:thr-1", "topic:t2"},
	}

	ev := buildCardUpdatedEvent(board, prev, upd, []string{"assignee_refs", "related_refs"})
	payload, ok := ev["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected payload map, got %#v", ev["payload"])
	}

	if !reflect.DeepEqual(payload["previous_assignee_refs"], []any{"actor:alice"}) {
		t.Fatalf("previous_assignee_refs: %#v", payload["previous_assignee_refs"])
	}
	if !reflect.DeepEqual(payload["assignee_refs"], []any{"actor:bob"}) {
		t.Fatalf("assignee_refs: %#v", payload["assignee_refs"])
	}
	if !reflect.DeepEqual(payload["previous_related_refs"], []any{"thread:thr-1", "topic:t1"}) {
		t.Fatalf("previous_related_refs: %#v", payload["previous_related_refs"])
	}
	if !reflect.DeepEqual(payload["related_refs"], []any{"thread:thr-1", "topic:t2"}) {
		t.Fatalf("related_refs: %#v", payload["related_refs"])
	}
	if _, ok := payload["parent_thread"]; ok {
		t.Fatalf("expected payload to omit parent_thread, got %#v", payload)
	}
}
