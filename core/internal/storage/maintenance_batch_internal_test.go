package storage

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/sqliteutil"
)

func TestAdaptiveMaintenanceBatchOverrunAndMinimum(t *testing.T) {
	var batch adaptiveMaintenanceBatch
	done, err := batch.run(context.Background(), 3, func(ctx context.Context, limit int) (bool, error) {
		if limit != 3 {
			t.Fatalf("initial limit=%d", limit)
		}
		if _, ok := ctx.Deadline(); ok || ctx.Done() != nil {
			t.Fatal("chunk context can be cancelled")
		}
		time.Sleep(maintenanceChunkLease + 20*time.Millisecond)
		return true, nil
	})
	if err != nil || !done || batch.limit.Load() != 1 {
		t.Fatalf("overrun: done=%v err=%v cap=%d", done, err, batch.limit.Load())
	}
	done, err = batch.run(context.Background(), 200, func(ctx context.Context, limit int) (bool, error) {
		if limit != 1 {
			t.Fatalf("minimum cap was lost: %d", limit)
		}
		return true, nil
	})
	if err != nil || !done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	var unchanged adaptiveMaintenanceBatch
	_, _ = unchanged.run(context.Background(), 200, func(ctx context.Context, limit int) (bool, error) { return false, sql.ErrTxDone })
	if unchanged.limit.Load() != 200 {
		t.Fatalf("fast failure reduced cap=%d", unchanged.limit.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ran := false
	if _, err = unchanged.run(ctx, 200, func(ctx context.Context, limit int) (bool, error) { ran = true; return false, nil }); !errors.Is(err, context.Canceled) || ran {
		t.Fatalf("cancelled caller started a chunk: ran=%v err=%v", ran, err)
	}
}

func TestAdaptiveMaintenanceSmallCallerDoesNotLowerCap(t *testing.T) {
	var batch adaptiveMaintenanceBatch
	for _, requested := range []int{1, 200} {
		_, err := batch.run(context.Background(), requested, func(ctx context.Context, limit int) (bool, error) {
			if limit != requested {
				t.Fatalf("requested=%d limit=%d", requested, limit)
			}
			return false, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if batch.limit.Load() != 200 {
		t.Fatalf("successful small call lowered cap=%d", batch.limit.Load())
	}
}

// A chunk whose lease expires while it is between statements of a write
// transaction must finish and commit, leaving the database writable from
// another connection; nothing interrupts its SQL.
func TestAdaptiveMaintenanceLeaseExpiryKeepsDatabaseWritable(t *testing.T) {
	t.Parallel()
	w, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err = w.DB().Exec(`CREATE TABLE maintenance_lease_probe(n INTEGER)`); err != nil {
		t.Fatal(err)
	}
	var batch adaptiveMaintenanceBatch
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = batch.run(parent, 200, func(ctx context.Context, limit int) (bool, error) {
		tx, cleanup, err := sqliteutil.BeginMaintenanceChunkWithBusyWait(ctx, w.DB(), 20*time.Millisecond)
		if err != nil {
			return false, err
		}
		defer cleanup()
		if _, err = tx.ExecContext(ctx, `INSERT INTO maintenance_lease_probe VALUES(1)`); err != nil {
			return false, err
		}
		time.Sleep(maintenanceChunkLease + 20*time.Millisecond)
		// A heavy statement after the lease has expired.
		if _, err = tx.ExecContext(ctx, `WITH RECURSIVE n(v) AS (VALUES(2) UNION ALL SELECT v+1 FROM n WHERE v<20000) INSERT INTO maintenance_lease_probe SELECT v FROM n`); err != nil {
			return false, err
		}
		return true, tx.Commit()
	})
	if err != nil {
		t.Fatalf("lease expiry interrupted the chunk: %v", err)
	}
	var rows int
	if err = w.DB().QueryRow(`SELECT count(*) FROM maintenance_lease_probe`).Scan(&rows); err != nil || rows != 20000 {
		t.Fatalf("chunk did not commit: rows=%d err=%v", rows, err)
	}
	other, err := sql.Open("sqlite", sqliteDSN(w.Layout().DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	conn, err := other.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, q := range []string{`PRAGMA busy_timeout=500`, `BEGIN IMMEDIATE`, `ROLLBACK`} {
		if _, err = conn.ExecContext(context.Background(), q); err != nil {
			t.Fatalf("database not writable after lease expiry: %s: %v", q, err)
		}
	}
	if batch.limit.Load() != 100 {
		t.Fatalf("overrun did not reduce cap: %d", batch.limit.Load())
	}
}
