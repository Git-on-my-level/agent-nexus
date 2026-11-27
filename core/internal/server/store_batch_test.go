package server

import (
	"context"
	"testing"
	"time"
)

// Inside a store batch the batch owns the quota lock. RefreshThread must not
// then wait for stepMu: the background worker holds stepMu while it waits for
// the quota lock, so waiting here would deadlock both.
func TestRefreshThreadInsideStoreBatchDoesNotTakeStepMu(t *testing.T) {
	m := &ProjectionMaintainer{opts: handlerOptions{primitiveStore: batchGuardStore{}}}
	m.stepMu.Lock()
	defer m.stepMu.Unlock()
	done := make(chan error, 1)
	go func() {
		done <- m.RefreshThread(context.WithValue(context.Background(), storeBatchKey{}, true), "thread-1", time.Now())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RefreshThread waited for stepMu inside a store batch")
	}
}

// batchGuardStore panics if called: the guard must return before any store use.
type batchGuardStore struct{ PrimitiveStore }
