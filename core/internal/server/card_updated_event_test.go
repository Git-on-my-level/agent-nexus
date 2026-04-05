package server

import (
	"reflect"
	"testing"
)

func TestBuildCardUpdatedEventAssigneeRefsAndRelatedRefs(t *testing.T) {
	t.Parallel()

	board := map[string]any{"id": "board-1", "thread_id": "t-board", "refs": []any{}}
	prev := map[string]any{
		"id":            "c1",
		"thread_id":     "ct",
		"assignee":      "alice",
		"refs":          []any{"topic:t1"},
		"parent_thread": "thr-1",
	}
	upd := map[string]any{
		"id":            "c1",
		"thread_id":     "ct",
		"assignee":      "bob",
		"refs":          []any{"topic:t2"},
		"parent_thread": "thr-1",
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
}
