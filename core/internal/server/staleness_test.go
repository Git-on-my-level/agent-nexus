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
