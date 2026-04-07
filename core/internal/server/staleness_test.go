package server

import (
	"testing"
	"time"
)

func TestIsThreadStaleAtCadenceRules(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		thread         map[string]any
		lastActivityAt time.Time
		want           bool
	}{
		{
			name: "reactive threads never stale",
			thread: map[string]any{
				"cadence":          "reactive",
				"next_check_in_at": now.Add(-48 * time.Hour).Format(time.RFC3339),
			},
			want: false,
		},
		{
			name: "daily stale without recent receipt_or_decision",
			thread: map[string]any{
				"cadence":          "daily",
				"next_check_in_at": now.Add(-2 * time.Hour).Format(time.RFC3339),
			},
			lastActivityAt: now.Add(-48 * time.Hour),
			want:           true,
		},
		{
			name: "daily not stale with recent receipt_or_decision",
			thread: map[string]any{
				"cadence":          "daily",
				"next_check_in_at": now.Add(-2 * time.Hour).Format(time.RFC3339),
			},
			lastActivityAt: now.Add(-6 * time.Hour),
			want:           false,
		},
		{
			name: "weekly stale when activity outside 7d",
			thread: map[string]any{
				"cadence":          "weekly",
				"next_check_in_at": now.Add(-12 * time.Hour).Format(time.RFC3339),
			},
			lastActivityAt: now.Add(-8 * 24 * time.Hour),
			want:           true,
		},
		{
			name: "monthly not stale when activity inside 30d",
			thread: map[string]any{
				"cadence":          "monthly",
				"next_check_in_at": now.Add(-12 * time.Hour).Format(time.RFC3339),
			},
			lastActivityAt: now.Add(-10 * 24 * time.Hour),
			want:           false,
		},
		{
			name: "custom cadence window anchored to next_check_in_at",
			thread: map[string]any{
				"cadence":          "custom",
				"next_check_in_at": now.Add(-24 * time.Hour).Format(time.RFC3339),
			},
			lastActivityAt: now.Add(-2 * 24 * time.Hour),
			want:           false,
		},
		{
			name: "custom stale when activity older than anchored custom window",
			thread: map[string]any{
				"cadence":          "custom",
				"next_check_in_at": now.Add(-24 * time.Hour).Format(time.RFC3339),
			},
			lastActivityAt: now.Add(-10 * 24 * time.Hour),
			want:           true,
		},
		{
			name: "cron cadence stale when activity before previous run",
			thread: map[string]any{
				"cadence":          "0 * * * *",
				"next_check_in_at": now.Add(-2 * time.Hour).Format(time.RFC3339),
			},
			lastActivityAt: now.Add(-2 * time.Hour),
			want:           true,
		},
		{
			name: "cron cadence not stale when activity after previous run",
			thread: map[string]any{
				"cadence":          "0 * * * *",
				"next_check_in_at": now.Add(-2 * time.Hour).Format(time.RFC3339),
			},
			lastActivityAt: now.Add(-30 * time.Minute),
			want:           false,
		},
		{
			name: "invalid cadence is treated as not stale",
			thread: map[string]any{
				"cadence":          "not-a-cadence",
				"next_check_in_at": now.Add(-24 * time.Hour).Format(time.RFC3339),
			},
			lastActivityAt: now.Add(-72 * time.Hour),
			want:           false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isThreadStaleAt(now, tc.thread, tc.lastActivityAt)
			if got != tc.want {
				t.Fatalf("unexpected stale result: got %v want %v", got, tc.want)
			}
		})
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
			name: "actor statement counts as activity",
			event: map[string]any{
				"type":      "actor_statement",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
			},
			want: true,
		},
		{
			name: "review completed counts as activity",
			event: map[string]any{
				"type":      "review_completed",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
			},
			want: true,
		},
		{
			name: "document update counts as activity",
			event: map[string]any{
				"type":      "document_updated",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
			},
			want: true,
		},
		{
			name: "intervention needed is meaningful activity",
			event: map[string]any{
				"type":      "intervention_needed",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
			},
			want: true,
		},
		{
			name: "inbox ack is coordination noise",
			event: map[string]any{
				"type":      "inbox_item_acknowledged",
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
		{
			name: "thread_updated open_cards only is coordination noise",
			event: map[string]any{
				"type":      "thread_updated",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
				"payload":   map[string]any{"changed_fields": []string{"open_cards"}},
			},
			want: false,
		},
		{
			name: "thread_created is not follow up activity",
			event: map[string]any{
				"type":      "thread_created",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
				"summary":   "thread created",
				"payload":   map[string]any{"changed_fields": []string{"title", "status"}},
			},
			want: false,
		},
		{
			name: "legacy snapshot events are ignored",
			event: map[string]any{
				"type":      "snapshot_updated",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
				"summary":   "legacy snapshot event",
			},
			want: false,
		},
		{
			name: "thread_updated with substantive fields counts as activity",
			event: map[string]any{
				"type":      "thread_updated",
				"thread_id": "thread-1",
				"ts":        "2026-03-04T12:00:00Z",
				"summary":   "thread updated",
				"payload":   map[string]any{"changed_fields": []string{"title"}},
			},
			want: true,
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
