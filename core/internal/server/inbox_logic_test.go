package server

import "testing"

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
