package server

import (
	"reflect"
	"testing"
	"time"

	"organization-autorunner-core/internal/primitives"
)

func TestMakeInboxItemIDDeterministic(t *testing.T) {
	t.Parallel()

	first := makeInboxItemID("decision_needed", "thread-1", "", "event-1")
	second := makeInboxItemID("decision_needed", "thread-1", "", "event-1")
	if first != second {
		t.Fatalf("expected deterministic inbox id, got %q and %q", first, second)
	}

	want := "inbox:decision_needed:thread-1:none:event-1"
	if first != want {
		t.Fatalf("unexpected inbox id: got %q want %q", first, want)
	}
}

func TestMakeInboxItemIDDefaultsNone(t *testing.T) {
	t.Parallel()

	got := makeInboxItemID("work_item_risk", "thread-1", "", "")
	want := "inbox:work_item_risk:thread-1:none:none"
	if got != want {
		t.Fatalf("unexpected inbox id defaults: got %q want %q", got, want)
	}
}

func TestLatestInboxAcknowledgmentsMapsLegacyRiskReviewIDs(t *testing.T) {
	t.Parallel()

	ackedAt := latestInboxAcknowledgments([]map[string]any{
		{
			"type": "inbox_item_acknowledged",
			"ts":   "2026-04-05T00:00:00Z",
			"refs": []any{"inbox:" + makeInboxItemID("risk_review", "thread-1", "card-1", "")},
		},
	})

	canonicalID := makeInboxItemID("work_item_risk", "thread-1", "card-1", "")
	acked, ok := ackedAt[canonicalID]
	if !ok {
		t.Fatalf("expected canonical work_item_risk id %q to be ack-suppressed, got %#v", canonicalID, ackedAt)
	}
	if !acked.Equal(time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected acknowledgment time: %#v", acked)
	}
}

func TestDeriveEventBackedInboxItemContractFields(t *testing.T) {
	t.Parallel()

	ev := map[string]any{
		"type":      "decision_needed",
		"id":        "evt-decide-1",
		"thread_id": "thr-1",
		"ts":        "2026-04-05T12:00:00Z",
		"refs":      []any{"topic:top-1", "document:doc-9"},
		"summary":   "Need a decision",
	}
	item, ok := deriveEventBackedInboxItem(ev)
	if !ok {
		t.Fatal("expected derived item")
	}
	if got := item.Data["subject_ref"]; got != "topic:top-1" {
		t.Fatalf("subject_ref: got %#v", got)
	}
	if got := item.Data["source_event_ref"]; got != "event:evt-decide-1" {
		t.Fatalf("source_event_ref: got %#v", got)
	}
	rr, err := extractStringSlice(item.Data["related_refs"])
	if err != nil {
		t.Fatalf("related_refs: %v", err)
	}
	wantRR := []string{"document:doc-9", "thread:thr-1", "topic:top-1"}
	if !reflect.DeepEqual(rr, wantRR) {
		t.Fatalf("related_refs: got %#v want %#v", rr, wantRR)
	}
}

func TestDeriveEventBackedInboxItemSubjectFallsBackToThread(t *testing.T) {
	t.Parallel()

	ev := map[string]any{
		"type":      "intervention_needed",
		"id":        "evt-int-2",
		"thread_id": "thr-z",
		"ts":        "2026-04-05T12:00:00Z",
		"refs":      []any{"inbox:inbox:decision_needed:thr-z:none:e1"},
		"summary":   "Act",
	}
	item, ok := deriveEventBackedInboxItem(ev)
	if !ok {
		t.Fatal("expected derived item")
	}
	if got := item.Data["subject_ref"]; got != "thread:thr-z" {
		t.Fatalf("subject_ref: got %#v want thread:thr-z", got)
	}
}

func TestDeriveWorkItemRiskInboxItemContractFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	horizon := 7 * 24 * time.Hour
	card := map[string]any{
		"id":                 "card-42",
		"parent_thread":      "thr-board",
		"board_id":           "brd-1",
		"title":              "Ship fix",
		"column_key":         "ready",
		"due_at":             now.Add(24 * time.Hour).Format(time.RFC3339),
		"updated_at":         now.Format(time.RFC3339),
		"refs":               []any{"topic:top-77"},
		"related_refs":       []any{"document:doc-2"},
		"pinned_document_id": "doc-pin",
	}
	item, ok := deriveWorkItemRiskInboxItem(card, now, horizon)
	if !ok {
		t.Fatal("expected work item risk")
	}
	if got := item.Data["subject_ref"]; got != "card:card-42" {
		t.Fatalf("subject_ref: got %#v", got)
	}
	rr, err := extractStringSlice(item.Data["related_refs"])
	if err != nil {
		t.Fatalf("related_refs: %v", err)
	}
	wantRR := []string{"board:brd-1", "document:doc-2", "document:doc-pin", "thread:thr-board", "topic:top-77"}
	if !reflect.DeepEqual(rr, wantRR) {
		t.Fatalf("related_refs: got %#v want %#v", rr, wantRR)
	}
}

func TestPayloadFromDerivedInboxItemBackfillsLegacyShape(t *testing.T) {
	t.Parallel()

	item := primitives.DerivedInboxItem{
		ID:            "inbox:work_item_risk:thr-1:card-9:none",
		ThreadID:      "thr-1",
		Category:      "work_item_risk",
		SourceCardID:  "card-9",
		TriggerAt:     "2026-04-05T00:00:00Z",
		SourceEventID: "",
		Data: map[string]any{
			"id":                 "inbox:work_item_risk:thr-1:card-9:none",
			"category":           "work_item_risk",
			"thread_id":          "thr-1",
			"card_id":            "card-9",
			"board_id":           "brd-9",
			"title":              "Legacy row",
			"recommended_action": "follow_up_work_item",
		},
	}
	out := payloadFromDerivedInboxItem(item)
	if got := out["subject_ref"]; got != "card:card-9" {
		t.Fatalf("subject_ref: got %#v", got)
	}
	rr, err := extractStringSlice(out["related_refs"])
	if err != nil || len(rr) == 0 {
		t.Fatalf("related_refs: %#v err=%v", out["related_refs"], err)
	}
	if rr[0] != "board:brd-9" || rr[1] != "thread:thr-1" {
		t.Fatalf("unexpected related_refs order/content: %#v", rr)
	}

	evItem := primitives.DerivedInboxItem{
		ID:            "inbox:decision_needed:thr-x:none:evt-old",
		ThreadID:      "thr-x",
		Category:      "decision_needed",
		SourceEventID: "evt-old",
		TriggerAt:     "2026-04-05T01:00:00Z",
		Data: map[string]any{
			"id":                 "inbox:decision_needed:thr-x:none:evt-old",
			"category":           "decision_needed",
			"thread_id":          "thr-x",
			"source_event_id":    "evt-old",
			"title":              "Old",
			"recommended_action": "make_decision",
		},
	}
	out2 := payloadFromDerivedInboxItem(evItem)
	if got := out2["subject_ref"]; got != "thread:thr-x" {
		t.Fatalf("event legacy subject_ref: got %#v", got)
	}
	if got := out2["source_event_ref"]; got != "event:evt-old" {
		t.Fatalf("source_event_ref: got %#v", got)
	}
	rr2, _ := extractStringSlice(out2["related_refs"])
	if len(rr2) != 1 || rr2[0] != "thread:thr-x" {
		t.Fatalf("expected thread-only related_refs, got %#v", rr2)
	}
}
