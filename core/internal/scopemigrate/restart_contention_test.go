package scopemigrate_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/url"
	"testing"
	"time"

	"agent-nexus-core/internal/scopemigrate"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
	"modernc.org/sqlite"
)

func TestUnchangedFencedRestartPreservesProgress(t *testing.T) {
	w, r := metadataFixture(t, 100)
	ctx := context.Background()
	must(t, w.InstallScopeFormatFence(ctx))
	// Fence DDL changes the schema cookie and ledger table name. Capture the
	// final schema before staging, as the explicit integration protocol requires.
	tx, err := w.DB().BeginTx(ctx, nil)
	must(t, err)
	must(t, storage.InstallScopeMigrationEpoch(ctx, tx))
	must(t, tx.Commit())
	token := begin(t, r)
	before, err := r.Step(ctx, token, 32)
	must(t, err)
	if before.Processed != 32 {
		t.Fatal(before)
	}
	root := w.Layout().RootDir
	for attempt := 0; attempt < 2; attempt++ {
		must(t, w.Close())
		w, err = storage.InitializeWorkspace(ctx, root)
		must(t, err)
		t.Cleanup(func() { w.Close() })
		r.DB = w.DB()
		if epoch := sourceEpoch(t, r.DB); epoch != before.SourceEpoch {
			t.Fatalf("unchanged fenced reopen advanced epoch: %d -> %d", before.SourceEpoch, epoch)
		}
		after, err := r.Report(ctx)
		must(t, err)
		if after != before {
			t.Fatalf("restart lost durable progress: before=%+v after=%+v", before, after)
		}
	}
	after, err := r.Step(ctx, token, 32)
	must(t, err)
	if after.Generation != before.Generation || after.SourceEpoch != before.SourceEpoch || after.Processed != 64 || after.Cursor <= before.Cursor {
		t.Fatalf("restart reset/skipped progress: before=%+v after=%+v", before, after)
	}
	// A real source mutation must still invalidate the staged generation.
	_, err = w.DB().ExecContext(ctx, `UPDATE artifacts SET content_type='changed' WHERE id='canonical-000000'`)
	must(t, err)
	changed, err := r.Step(ctx, token, 32)
	must(t, err)
	if changed.Generation != after.Generation+1 || changed.SourceEpoch <= after.SourceEpoch || changed.Processed != 0 {
		t.Fatal("real mutation did not restart generation", changed)
	}
}

func TestWriterContentionBoundsWorkerTransactions(t *testing.T) {
	for _, operation := range []string{"acquire", "step", "lifecycle", "shutdown"} {
		t.Run(operation, func(t *testing.T) {
			w, r := fixture(t, 100)
			ctx := context.Background()
			// A separate one-connection worker pool lets us observe its admission
			// PRAGMA and inspect that same connection after cleanup.
			dsn := &url.URL{Scheme: "file", Path: w.Layout().DatabasePath}
			query := dsn.Query()
			query.Add("_pragma", "busy_timeout(20000)")
			query.Add("_pragma", "journal_mode(WAL)")
			query.Set("_txlock", "immediate")
			dsn.RawQuery = query.Encode()
			pool, capture, err := perfguard.Open(dsn.String())
			must(t, err)
			defer pool.Close()
			pool.SetMaxOpenConns(1)
			pool.SetMaxIdleConns(1)
			r.DB = pool
			token := begin(t, r)
			// The production budget is what must bound the writer-lock wait.
			r.ChunkBudget = scopemigrate.DefaultChunkBudget
			before, err := r.Report(ctx)
			must(t, err)
			writer, err := w.DB().BeginTx(ctx, nil)
			must(t, err)
			defer writer.Rollback()
			// Warm the worker connection before exercising admission. Physical
			// driver initialization is outside this lock-wait regression.
			warm, err := pool.Conn(ctx)
			must(t, err)
			must(t, warm.Close())
			// Keep the writer held until the operation returns. At every actual
			// BEGIN, the worker must already have disabled the serving busy wait.
			// This verifies the mechanism without measuring host scheduling time.
			admitted, invalidAdmission := false, false
			var admissionErr error
			capture.BeforeBeginConn(func(ctx context.Context, native driver.Conn, _ driver.TxOptions) {
				rows, err := native.(driver.QueryerContext).QueryContext(ctx, "PRAGMA busy_timeout", nil)
				if err != nil {
					admissionErr = err
					if ctx.Err() != nil {
						admissionErr = ctx.Err()
					}
					return
				}
				var value [1]driver.Value
				err = rows.Next(value[:])
				closeErr := rows.Close()
				if err == nil {
					err = closeErr
				}
				if err != nil {
					admissionErr = err
					if ctx.Err() != nil {
						admissionErr = ctx.Err()
					}
					return
				}
				admitted = true
				if timeout, ok := value[0].(int64); !ok || timeout != 0 {
					invalidAdmission = true
				}
			})
			var workerErr error
			switch operation {
			case "acquire":
				_, workerErr = r.Acquire(ctx)
			case "step":
				store := &scopemigrate.Store{Runner: r}
				result, e := store.Step(ctx, scopes.StepRequest{JobID: r.Job, LeaseToken: token, ExpectedEpoch: before.SourceEpoch, MaxRecords: 32, MaxBytes: int64(r.MaxBytes), MaxDuration: 50 * time.Millisecond})
				workerErr = e
				if result != (scopes.StepResult{}) {
					t.Errorf("contended step returned success: %+v", result)
				}
			case "lifecycle":
				r.Rebuilder = &lifecycle{}
				_, workerErr = r.LifecycleStep(ctx, token)
			case "shutdown":
				paused := make(chan struct{}, 1)
				workerCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
				worker, e := scopemigrate.StartWorker(workerCtx, true, r, time.Hour, func(string) { paused <- struct{}{} })
				must(t, e)
				select {
				case <-paused: // The first lease acquisition encountered contention.
				case <-workerCtx.Done():
				}
				workerErr = worker.Close()
			}
			capture.BeforeBeginConn(nil)
			if admissionErr != nil && !errors.Is(admissionErr, context.Canceled) && !errors.Is(admissionErr, context.DeadlineExceeded) {
				t.Error("could not inspect worker admission", admissionErr)
			}
			if invalidAdmission {
				t.Error("worker began before disabling the serving busy timeout")
			}
			if !admitted && !errors.Is(workerErr, context.Canceled) && !errors.Is(workerErr, context.DeadlineExceeded) {
				t.Error("worker did not reach admission or exhaust its context", workerErr)
			}
			if operation == "shutdown" {
				if !errors.Is(workerErr, context.Canceled) && !errors.Is(workerErr, context.DeadlineExceeded) {
					t.Errorf("shutdown: %v", workerErr)
				}
			} else {
				var busy *sqlite.Error
				if !(errors.As(workerErr, &busy) && busy.Code()&255 == 5) && !errors.Is(workerErr, context.DeadlineExceeded) && !errors.Is(workerErr, context.Canceled) {
					t.Errorf("expected busy or cancelled acquisition, got %v", workerErr)
				}
			}
			var timeout int
			must(t, pool.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout))
			if timeout != 20000 {
				t.Errorf("worker leaked connection timeout into serving pool: %d", timeout)
			}
			must(t, writer.Rollback())
			after, err := r.Report(ctx)
			must(t, err)
			if after != before {
				t.Errorf("contention advanced lease/watermark: before=%+v after=%+v", before, after)
			}
			// The post-contention data assertion uses the fixture's work budget,
			// not the 50ms production budget being tested above. Renew the lease
			// as a real worker does before resuming after a long scheduling gap.
			r.ChunkBudget = fixtureChunkBudget
			token, err = r.Acquire(ctx)
			must(t, err)
			resumed, err := r.Step(ctx, token, 32)
			must(t, err)
			if resumed.Processed != 32 || resumed.Generation != before.Generation {
				t.Fatal("did not resume after writer released", resumed)
			}
		})
	}
}
