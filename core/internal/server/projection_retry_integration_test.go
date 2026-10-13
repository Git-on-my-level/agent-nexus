package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

type isolatedProjectionFailureStore struct {
	PrimitiveStore
	failThread string
	attempts   map[string]int
}

func (s *isolatedProjectionFailureStore) PutDerivedTopicProjection(ctx context.Context, p primitives.DerivedTopicProjection) error {
	s.attempts[p.ThreadID]++
	if p.ThreadID == s.failThread {
		return errors.New("injected permanent projection failure")
	}
	return s.PrimitiveStore.PutDerivedTopicProjection(ctx, p)
}

func topicProjectionRetryAfter(t *testing.T, ctx context.Context, h manualProjectionHarness, threadID string) time.Time {
	t.Helper()
	var retry int64
	if err := h.workspace.DB().QueryRowContext(ctx, "SELECT retry_after FROM topic_projection_refresh_status WHERE thread_id=?", threadID).Scan(&retry); err != nil {
		t.Fatal(err)
	}
	if retry <= 0 {
		t.Fatalf("missing retry_after for %s", threadID)
	}
	return time.UnixMilli(retry).UTC()
}

func TestProjectionRetryFailingFirstItemDoesNotBlockHealthyWork(t *testing.T) {
	h := newManualProjectionTestServer(t)
	ctx := context.Background()
	bad := createBoardThreadViaHTTP(t, h.primitivesTestHarness, "Failing first")
	good := createBoardThreadViaHTTP(t, h.primitivesTestHarness, "Healthy second")
	now := time.Now().UTC()
	// Control queue ordering independently of thread identifiers or wall-clock speed.
	for _, query := range []string{"DELETE FROM derived_topic_dirty_queue", "DELETE FROM topic_projection_refresh_status"} {
		if _, err := h.workspace.DB().ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.store.MarkTopicProjectionsDirty(ctx, []string{bad}, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.store.MarkTopicProjectionsDirty(ctx, []string{good}, now); err != nil {
		t.Fatal(err)
	}
	store := &isolatedProjectionFailureStore{PrimitiveStore: h.store, failThread: bad, attempts: map[string]int{}}
	maintainer := NewProjectionMaintainer(ProjectionMaintainerConfig{PrimitiveStore: store, DirtyBatchSize: 2})
	if err := maintainer.Step(ctx, now); err == nil {
		t.Fatal("expected failed-thread error")
	}
	statuses, err := h.store.GetTopicProjectionRefreshStatuses(ctx, []string{bad, good})
	if err != nil {
		t.Fatal(err)
	}
	if statuses[good].IsDirty() || store.attempts[good] != 1 {
		t.Fatalf("healthy item behind failure did not advance: %#v attempts=%v", statuses, store.attempts)
	}
	if !statuses[bad].IsDirty() || statuses[bad].InProgress() || statuses[bad].LastErrorMessage == "" {
		t.Fatalf("failed item lost visible error/dirty status: %#v", statuses[bad])
	}
	// Restart the worker and remove the queue row, as in crash recovery. Durable
	// generation fallback must preserve both work and its retry eligibility.
	if err := h.store.ClearDerivedTopicProjectionDirty(ctx, bad); err != nil {
		t.Fatal(err)
	}
	maintainer = NewProjectionMaintainer(ProjectionMaintainerConfig{PrimitiveStore: store, DirtyBatchSize: 1})
	if err := h.store.MarkTopicProjectionsDirty(ctx, []string{good}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := maintainer.Step(ctx, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if store.attempts[bad] != 1 || store.attempts[good] != 2 {
		t.Fatalf("backoff starved healthy item at batch size 1: %v", store.attempts)
	}
	stats, err := h.store.GetDerivedTopicProjectionQueueStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PendingCount != 1 || stats.OldestDirtyAt != now.Add(-time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("retry hid backlog or reset its age: %#v", stats)
	}
	firstRetryAt := topicProjectionRetryAfter(t, ctx, h, bad)
	if err := maintainer.Step(ctx, firstRetryAt); err == nil {
		t.Fatal("expected eligible retry to fail")
	}
	if store.attempts[bad] != 2 {
		t.Fatalf("retry was not attempted: %v", store.attempts)
	}
	secondRetryAt := topicProjectionRetryAfter(t, ctx, h, bad)
	if delay := time.Until(secondRetryAt); delay < 9*time.Second || delay > 11*time.Second {
		t.Fatalf("second failure delay %s, want ~10s", delay)
	}
	// A new mutation must preserve the poison item's backoff, not make it starve
	// the queue again. Error details remain until a successful refresh.
	if err := h.store.MarkTopicProjectionsDirty(ctx, []string{bad}, secondRetryAt.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := maintainer.RunFullRebuild(ctx, secondRetryAt.Add(-time.Second), "actor"); err == nil {
		t.Fatal("full rebuild falsely reported success while retries were delayed")
	}
	store.failThread = ""
	if err := maintainer.Step(ctx, secondRetryAt.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if store.attempts[bad] != 2 {
		t.Fatal("second failure did not double retry delay")
	}
	if err := maintainer.Step(ctx, secondRetryAt); err != nil {
		t.Fatal(err)
	}
	statuses, err = h.store.GetTopicProjectionRefreshStatuses(ctx, []string{bad})
	if err != nil {
		t.Fatal(err)
	}
	if statuses[bad].IsDirty() || statuses[bad].LastErrorMessage != "" || statuses[bad].InProgress() {
		t.Fatalf("successful retry did not clear error and finish current generation: %#v", statuses[bad])
	}
	var count, retry int64
	if err := h.workspace.DB().QueryRowContext(ctx, "SELECT failure_count,retry_after FROM topic_projection_refresh_status WHERE thread_id=?", bad).Scan(&count, &retry); err != nil {
		t.Fatal(err)
	}
	if count != 0 || retry != 0 {
		t.Fatalf("success did not reset retry metadata: %d %d", count, retry)
	}
}

func TestProjectionRetryDeadlineUsesFailureTimeNotBatchStart(t *testing.T) {
	h := newManualProjectionTestServer(t)
	ctx := context.Background()
	bad := createBoardThreadViaHTTP(t, h.primitivesTestHarness, "Stale batch failure")
	good := createBoardThreadViaHTTP(t, h.primitivesTestHarness, "Healthy after stale batch")
	now := time.Now().UTC()
	for _, query := range []string{"DELETE FROM derived_topic_dirty_queue", "DELETE FROM topic_projection_refresh_status"} {
		if _, err := h.workspace.DB().ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.store.MarkTopicProjectionsDirty(ctx, []string{bad}, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.store.MarkTopicProjectionsDirty(ctx, []string{good}, now); err != nil {
		t.Fatal(err)
	}
	store := &isolatedProjectionFailureStore{PrimitiveStore: h.store, failThread: bad, attempts: map[string]int{}}
	maintainer := NewProjectionMaintainer(ProjectionMaintainerConfig{PrimitiveStore: store, DirtyBatchSize: 1})

	// Simulate a Step whose batch clock is already older than the 5s initial backoff,
	// as happens when processDirtyQueue keeps the time at which the batch was loaded.
	batchStart := now.Add(-30 * time.Second)
	if err := maintainer.Step(ctx, batchStart); err == nil {
		t.Fatal("expected failed-thread error")
	}
	if store.attempts[bad] != 1 {
		t.Fatalf("expected one failure attempt, got %v", store.attempts)
	}

	// Run loops with a fresh time.Now() (and may already have a notify/ticker wakeup).
	if err := maintainer.Step(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if store.attempts[bad] != 1 || store.attempts[good] != 1 {
		t.Fatalf("retry deadline used batch start: %v", store.attempts)
	}

	retryAt := topicProjectionRetryAfter(t, ctx, h, bad)
	if !retryAt.After(time.Now().UTC()) {
		t.Fatalf("retry_after %s was not in the future at failure commit", retryAt.Format(time.RFC3339Nano))
	}
	if err := maintainer.Step(ctx, retryAt); err == nil {
		t.Fatal("expected eligible retry to fail")
	}
	if store.attempts[bad] != 2 {
		t.Fatalf("retry was not attempted after backoff: %v", store.attempts)
	}
}
