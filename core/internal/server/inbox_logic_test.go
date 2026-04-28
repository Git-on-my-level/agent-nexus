package server

import (
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestMakeInboxItemIDDeterministic(t *testing.T) {
	t.Parallel()

	first := makeInboxItemID("ask", "thread-1", "", "event-1")
	second := makeInboxItemID("ask", "thread-1", "", "event-1")
	if first != second {
		t.Fatalf("expected deterministic inbox id, got %q and %q", first, second)
	}

	want := "inbox:ask:thread-1:none:event-1"
	if first != want {
		t.Fatalf("unexpected inbox id: got %q want %q", first, want)
	}
}

func TestMakeInboxItemIDDefaultsNone(t *testing.T) {
	t.Parallel()

	got := makeInboxItemID("escalate", "thread-1", "", "")
	want := "inbox:escalate:thread-1:none:none"
	if got != want {
		t.Fatalf("unexpected inbox id defaults: got %q want %q", got, want)
	}
}

func TestDeriveHumanAttentionInboxItemContractFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	event := map[string]any{
		"id":        "evt-human-1",
		"thread_id": "thr-card",
		"ts":        now.Format(time.RFC3339),
		"refs":      []any{"thread:thr-card", "topic:top-77"},
		"payload": map[string]any{
			"kind":               "review",
			"title":              "Review launch notes",
			"subject_ref":        "topic:top-77",
			"related_refs":       []any{"document:doc-2"},
			"requester_actor_id": "actor-agent",
			"requester_agent_id": "agent-a",
			"response_proposals": []any{"Approved as written.", "Request changes to section 2."},
		},
	}
	item, ok := deriveHumanAttentionInboxItem(event)
	if !ok {
		t.Fatal("expected human attention item")
	}
	if got := item.Data["subject_ref"]; got != "topic:top-77" {
		t.Fatalf("subject_ref: got %#v", got)
	}
	rr, err := extractStringSlice(item.Data["related_refs"])
	if err != nil {
		t.Fatalf("related_refs: %v", err)
	}
	if len(rr) != 3 || rr[0] != "document:doc-2" || rr[1] != "thread:thr-card" || rr[2] != "topic:top-77" {
		t.Fatalf("related_refs: got %#v", rr)
	}
	if got := item.Category; got != "review" {
		t.Fatalf("category/kind: got %#v want review", got)
	}
	props, err := extractStringSlice(item.Data["response_proposals"])
	if err != nil || len(props) != 2 || props[0] != "Approved as written." {
		t.Fatalf("response_proposals: %#v err=%v", item.Data["response_proposals"], err)
	}
}

func TestPayloadFromDerivedInboxItemUsesColumnKind(t *testing.T) {
	t.Parallel()

	item := primitives.DerivedInboxItem{
		ID:            "inbox:escalate:thr-1:card-9:none",
		ThreadID:      "thr-1",
		Category:      "escalate",
		SourceCardID:  "card-9",
		TriggerAt:     "2026-04-05T00:00:00Z",
		SourceEventID: "",
		Data: map[string]any{
			"board_id": "brd-9",
			"title":    "Card risk",
		},
	}
	out := payloadFromDerivedInboxItem(item)
	if got := out["kind"]; got != "escalate" {
		t.Fatalf("kind: got %#v want escalate", got)
	}
	if got := out["subject_ref"]; got != "thread:thr-1" {
		t.Fatalf("subject_ref: got %#v", got)
	}
	rr, err := extractStringSlice(out["related_refs"])
	if err != nil || len(rr) == 0 {
		t.Fatalf("related_refs: %#v err=%v", out["related_refs"], err)
	}
	if len(rr) != 1 || rr[0] != "thread:thr-1" {
		t.Fatalf("unexpected related_refs order/content: %#v", rr)
	}

	evItem := primitives.DerivedInboxItem{
		ID:            "inbox:ask:thr-x:none:evt-old",
		ThreadID:      "thr-x",
		Category:      "ask",
		SourceEventID: "evt-old",
		TriggerAt:     "2026-04-05T01:00:00Z",
		Data: map[string]any{
			"title": "Old",
		},
	}
	out2 := payloadFromDerivedInboxItem(evItem)
	if got := out2["kind"]; got != "ask" {
		t.Fatalf("kind: got %#v want ask", got)
	}
	if got := out2["subject_ref"]; got != "thread:thr-x" {
		t.Fatalf("event subject_ref: got %#v", got)
	}
	if got := out2["source_event_ref"]; got != "event:evt-old" {
		t.Fatalf("source_event_ref: got %#v", got)
	}
	rr2, _ := extractStringSlice(out2["related_refs"])
	if len(rr2) != 1 || rr2[0] != "thread:thr-x" {
		t.Fatalf("expected thread-only related_refs, got %#v", rr2)
	}
}
