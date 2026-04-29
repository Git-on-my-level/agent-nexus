package server

import (
	"testing"
	"time"
)

func TestIsThreadStaleAtIgnoresLegacyCadenceFields(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	thread := map[string]any{
		"cadence":          "daily",
		"next_check_in_at": now.Add(-48 * time.Hour).Format(time.RFC3339),
	}
	if isThreadStaleAt(now, thread, now.Add(-720*time.Hour)) {
		t.Fatal("expected dumb-thread model to never mark stale from cadence JSON")
	}
}

func TestIsMeaningfulThreadActivityEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event map[string]any
		want  bool
	}{
		{
			name: "card updated counts as activity",
			event: map[string]any{
				"type":      "card_updated",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
			},
			want: true,
		},
		{
			name: "card moved counts as activity",
			event: map[string]any{
				"type":      "card_moved",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
			},
			want: true,
		},
		{
			name: "card archived counts as activity",
			event: map[string]any{
				"type":      "card_archived",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
			},
			want: true,
		},
		{
			name: "document revision counts as activity",
			event: map[string]any{
				"type":      "document_revised",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
			},
			want: true,
		},
		{
			name: "card resolved counts as activity",
			event: map[string]any{
				"type":      "card_resolved",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
			},
			want: true,
		},
		{
			name: "human attention requested is coordination noise",
			event: map[string]any{
				"type":      "human_attention_requested",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
			},
			want: false,
		},
		{
			name: "human attention response is coordination noise",
			event: map[string]any{
				"type":      "human_attention_responded",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
			},
			want: false,
		},
		{
			name: "stale exception is coordination noise",
			event: map[string]any{
				"type":      "exception_raised",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
				"payload":   map[string]any{"subtype": "stale_topic"},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isMeaningfulThreadActivityEvent(tc.event); got != tc.want {
				t.Fatalf("unexpected result: got %v want %v for event %#v", got, tc.want, tc.event)
			}
		})
	}
}

func TestLatestThreadActivityFromCardsPrefersCanonicalRelatedThread(t *testing.T) {
	t.Parallel()

	relatedTS := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	backingTS := time.Date(2026, 4, 7, 11, 0, 0, 0, time.UTC)

	got := latestThreadActivityFromCards([]map[string]any{
		{
			"thread_id":    "card-thread",
			"related_refs": []any{"topic:t1", "thread:topic-thread"},
			"updated_at":   relatedTS.Format(time.RFC3339),
		},
		{
			"thread_id":  "card-only-thread",
			"updated_at": backingTS.Format(time.RFC3339),
		},
	})

	if _, ok := got["card-thread"]; ok {
		t.Fatalf("expected related thread to receive activity, got %#v", got)
	}
	if activity := got["topic-thread"]; !activity.Equal(relatedTS) {
		t.Fatalf("expected topic-thread activity at %v, got %v", relatedTS, activity)
	}
	if activity := got["card-only-thread"]; !activity.Equal(backingTS) {
		t.Fatalf("expected backing-thread fallback activity at %v, got %v", backingTS, activity)
	}
}
