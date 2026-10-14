package storage

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"modernc.org/sqlite"
	"sync/atomic"
	"testing"
	"time"
)

func compactRetrySchedule(budget time.Duration) compactMigrationSchedule {
	return compactMigrationSchedule{budget: budget, initialDelay: time.Millisecond, batchDelay: time.Millisecond, retryMin: 20 * time.Millisecond, retryMax: 80 * time.Millisecond}
}

func TestCompactMigrationRetriesHeldWriterAndCertifies(t *testing.T) {
	w := compactFixture(t, 450)
	held, err := w.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	// Confirm the exact install operation fails with a retryable SQLite cause.
	err = w.InitializeCompactAccessShadow(context.Background())
	if err == nil || !compactMigrationRetryable(err) {
		t.Fatalf("expected contention: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan struct{})
	reports := 0
	var proof CompactAccessVerification
	var failure error
	go func() {
		defer close(finished)
		w.runCompactAccessMigration(ctx, compactRetrySchedule(4*time.Second), func(p CompactAccessVerification, e error) { reports++; proof, failure = p, e })
	}()
	defer func() { cancel(); <-finished }()
	select {
	case <-finished:
		t.Fatal("worker gave up while writer was held")
	case <-time.After(200 * time.Millisecond):
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("worker did not certify after contention cleared")
	}
	if reports != 1 || failure != nil || proof.Rows != 450 {
		t.Fatalf("reports=%d proof=%+v error=%v", reports, proof, failure)
	}
	var mode int
	if err = w.db.QueryRow(`SELECT reader FROM resource_access_compact_control`).Scan(&mode); err != nil || mode != 1 {
		t.Fatalf("reader=%d error=%v", mode, err)
	}
}

func TestCompactMigrationBudgetExhaustionReportsOnce(t *testing.T) {
	w := compactFixture(t, 1)
	held, err := w.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	reports := 0
	var failure error
	started := time.Now()
	w.runCompactAccessMigration(context.Background(), compactRetrySchedule(150*time.Millisecond), func(_ CompactAccessVerification, e error) { reports++; failure = e })
	var diagnostic *CompactAccessMigrationError
	if reports != 1 || !errors.As(failure, &diagnostic) || diagnostic.Stage != "shadow_install" || diagnostic.Reason != "budget_exhausted_write_contention_retry" || diagnostic.SQLiteCode&255 != 5 {
		t.Fatalf("reports=%d error=%v", reports, failure)
	}
	if time.Since(started) > time.Second {
		t.Fatal("budget did not bound worker")
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	// A failed install must neither leave mirrors nor publish the reader.
	var mode, mirrors int
	if err = w.db.QueryRow(`SELECT reader,(SELECT count(*) FROM sqlite_schema WHERE name='access_compact_insert') FROM resource_access_compact_control`).Scan(&mode, &mirrors); err != nil || mode != 0 || mirrors != 0 {
		t.Fatalf("mode=%d mirrors=%d err=%v", mode, mirrors, err)
	}
}

func TestCompactMigrationFinalExistingModeAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mode      int
		cancelled bool
		reason    string
	}{
		{"certified", 1, false, ""}, {"operator rollback", 2, false, "operator_rollback"}, {"cancelled", 0, true, "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := compactFixture(t, 1)
			if tc.mode == 1 {
				proof := compactCertify(t, w)
				if err := w.CutoverCompactAccess(context.Background(), proof); err != nil {
					t.Fatal(err)
				}
			}
			if tc.mode == 2 {
				if err := w.RollbackCompactAccess(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			reports := 0
			var failure error
			w.runCompactAccessMigration(ctx, compactRetrySchedule(time.Second), func(_ CompactAccessVerification, e error) { reports++; failure = e })
			if reports != 1 {
				t.Fatalf("reports=%d", reports)
			}
			if tc.reason == "" {
				if failure != nil {
					t.Fatal(failure)
				}
				return
			}
			var diagnostic *CompactAccessMigrationError
			if !errors.As(failure, &diagnostic) || diagnostic.Reason != tc.reason {
				t.Fatalf("error=%v", failure)
			}
		})
	}
}

var compactSlowFunctionID atomic.Uint64

// SQLite may return INTERRUPT or ErrTxDone when a chunk deadline expires.
// The worker must retain rollback/cursor atomicity and retry the learned cap.
func TestCompactMigrationRetriesAdaptiveLeaseDeadline(t *testing.T) {
	function := fmt.Sprintf("compact_retry_delay_%d", compactSlowFunctionID.Add(1))
	if err := sqlite.RegisterScalarFunction(function, 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
		time.Sleep(20 * time.Millisecond)
		return int64(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	w := compactFixture(t, 8)
	if err := w.InitializeCompactAccessShadow(context.Background()); err != nil {
		t.Fatal(err)
	}
	compactExec(t, w, `CREATE TRIGGER compact_retry_slow BEFORE INSERT ON resource_access_compact_edges BEGIN SELECT `+function+`(); END`)
	reports := 0
	var failure error
	var proof CompactAccessVerification
	w.runCompactAccessMigration(context.Background(), compactRetrySchedule(5*time.Second), func(p CompactAccessVerification, e error) { reports++; proof, failure = p, e })
	if reports != 1 || failure != nil || proof.Rows != 8 || w.compactAccessBatch.limit.Load() >= 200 {
		t.Fatalf("reports=%d rows=%d cap=%d error=%v", reports, proof.Rows, w.compactAccessBatch.limit.Load(), failure)
	}
}
