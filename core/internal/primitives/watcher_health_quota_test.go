package primitives

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestWatcherQuotaEnabledPlacementAndRecoveryDoNotDeadlock(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	id := watcherLoopFixture(t, s, ctx, now)
	// Enable quota serialization without charging this fixture's existing rows.
	s.quota = WorkspaceQuota{MaxUploadBytes: 1024 * 1024}
	if _, err := s.AppendEvent(ctx, "placer", map[string]any{"type": "message_posted", "refs": []string{"card:" + id}, "payload": map[string]any{"text": "Active owner"}}); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		cfg := DefaultWatcherHealthConfig()
		if err := s.MaintainLoopHealth(bounded, now.Add(4*time.Minute), cfg); err != nil {
			done <- err
			return
		}
		if err := s.ReconcilePMHealth(bounded, true, now, cfg); err != nil {
			done <- err
			return
		}
		done <- s.ReconcilePMHealth(bounded, false, now.Add(time.Minute), cfg)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-bounded.Done():
		t.Fatal("quota-enabled alarm transaction deadlocked")
	}
	if got := attentionAll(t, s, ctx, "placer", "agent", "digest"); len(got) != 1 {
		t.Fatal("quota-enabled digest missing", got)
	}
	if n := watcherCount(t, s, "human_attention_withdrawn"); n != 1 {
		t.Fatal("quota-enabled recovery missing", n)
	}
}

func TestWatcherQuotaSerializesWithOrdinaryMutations(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	s.quota = WorkspaceQuota{MaxUploadBytes: 1024 * 1024}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	go func() {
		ready.Done()
		<-start
		for i := 0; i < 4; i++ {
			if err := s.ReconcilePMHealth(bounded, true, now.Add(time.Duration(i)*time.Hour), DefaultWatcherHealthConfig()); err != nil {
				results <- err
				return
			}
		}
		results <- nil
	}()
	go func() {
		ready.Done()
		<-start
		for i := 0; i < 8; i++ {
			if _, _, err := s.AppendEventWithReplay(bounded, "placer", fmt.Sprintf("ordinary-%d", i), "body-hash", map[string]any{"type": "message_posted", "refs": []string{}, "payload": map[string]any{"text": "ordinary mutation"}}, nil); err != nil {
				results <- err
				return
			}
		}
		results <- nil
	}()
	ready.Wait()
	close(start)
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-bounded.Done():
			t.Fatal("alarm and ordinary mutation lock order deadlocked")
		}
	}
	if n := watcherCount(t, s, "human_attention_requested"); n != 1 {
		t.Fatal("quota retries duplicated the alarm", n)
	}
	var receipts int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM idempotency_replays WHERE scope=? AND actor_id=?`, "events.create", "placer").Scan(&receipts); err != nil || receipts != 8 {
		t.Fatal("ordinary mutations were not retained", receipts, err)
	}
}

func TestWatcherQuotaRejectionRollsBackAlarm(t *testing.T) {
	s, ctx := attentionFixture(t)
	s.quota = WorkspaceQuota{MaxBlobBytes: 1}
	err := s.ReconcilePMHealth(ctx, true, time.Now().UTC(), DefaultWatcherHealthConfig())
	var quota *QuotaViolation
	if !errors.As(err, &quota) {
		t.Fatalf("want quota rejection, got %v", err)
	}
	if n := watcherCount(t, s, "human_attention_requested"); n != 0 {
		t.Fatal("quota rejection left a partial ask", n)
	}
	var n int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM watcher_health WHERE alarm_key='pm'`).Scan(&n); err != nil || n != 0 {
		t.Fatal("quota rejection advanced alarm state", n, err)
	}
}
