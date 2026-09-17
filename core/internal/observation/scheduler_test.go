package observation

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureReader struct {
	read func(context.Context, Target) (Report, error)
}

func (f fixtureReader) Capabilities() Capabilities {
	return Capabilities{Source: "fixture", ReadOne: true}
}
func (f fixtureReader) Read(ctx context.Context, t Target) (Report, error) { return f.read(ctx, t) }
func fixtureTarget() Target {
	return Target{WorkspaceID: "w", ConnectionID: "c", Source: "fixture", Kind: "issue", NativeID: "1"}
}
func fixturePolicy() RefreshPolicy {
	return RefreshPolicy{Interval: time.Minute, StaleAfter: 2 * time.Minute, Timeout: time.Second, MaxBackoff: time.Hour}
}
func TestSchedulerCoalescesAndRetainsLastGoodAcrossFailureAndRestart(t *testing.T) {
	scheduler, err := NewScheduler(2)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	scheduler.now = func() time.Time { return now }
	target := fixtureTarget()
	key := target.Key()
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	reader := fixtureReader{read: func(ctx context.Context, t Target) (Report, error) {
		calls.Add(1)
		close(entered)
		<-release
		return finishReport(newReport(t, "fixture"))
	}}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := scheduler.Refresh(context.Background(), key, fixturePolicy(), reader, target, true); err != nil {
			t.Error(err)
		}
	}()
	<-entered
	if state := scheduler.Snapshot()[key]; state.State != "running" {
		t.Fatalf("not running: %+v", state)
	}
	waiterDone := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		close(waiterDone)
		_, err := scheduler.Refresh(context.Background(), key, fixturePolicy(), reader, target, false)
		if err != nil {
			t.Error(err)
		}
	}()
	<-waiterDone
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("overlap: %d", calls.Load())
	}
	now = now.Add(2 * time.Minute)
	failed := fixtureReader{read: func(context.Context, Target) (Report, error) {
		return Report{}, &ReadError{Kind: ErrRateLimit, Message: "fixture", RetryAfter: 10 * time.Minute}
	}}
	result, err := scheduler.Refresh(context.Background(), key, fixturePolicy(), failed, target, true)
	if err == nil || result.Health.LastGood == nil || result.Health.LastError == nil || result.Health.NextDue.Before(now.Add(10*time.Minute)) {
		t.Fatalf("lost evidence or retry hint: %+v %v", result, err)
	}
	restored, _ := NewScheduler(1)
	restored.now = scheduler.now
	if err := restored.Restore(scheduler.Snapshot()); err != nil {
		t.Fatal(err)
	}
	result, err = restored.Refresh(context.Background(), key, fixturePolicy(), failed, target, true)
	if err != nil || !result.Skipped || result.Health.LastGood == nil {
		t.Fatalf("manual refresh bypassed backoff: %+v %v", result, err)
	}
}
func TestSchedulerTimeoutAndGlobalConcurrency(t *testing.T) {
	scheduler, _ := NewScheduler(1)
	target := fixtureTarget()
	started := make(chan struct{})
	release := make(chan struct{})
	reader := fixtureReader{read: func(ctx context.Context, t Target) (Report, error) {
		close(started)
		select {
		case <-ctx.Done():
			return Report{}, ctx.Err()
		case <-release:
			return newReport(t, "fixture"), nil
		}
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = scheduler.Refresh(context.Background(), target.Key(), fixturePolicy(), reader, target, true)
	}()
	<-started
	second := target
	second.NativeID = "2"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := scheduler.Refresh(ctx, second.Key(), fixturePolicy(), reader, second, true); err == nil {
		t.Fatal("global concurrency not bounded")
	}
	close(release)
	<-done
}
