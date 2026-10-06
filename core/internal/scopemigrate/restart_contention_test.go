package scopemigrate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/scopemigrate"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/storage"
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
			// Exactly one competing writer connection and one worker connection.
			// Verify the latter is returned with its normal serving timeout.
			w.DB().SetMaxOpenConns(2)
			w.DB().SetMaxIdleConns(2)
			token := begin(t, r)
			before, err := r.Report(ctx)
			must(t, err)
			writer, err := w.DB().BeginTx(ctx, nil)
			must(t, err)
			defer writer.Rollback()
			// Open the second connection before the timing window. Physical
			// driver initialization (especially under -race) is separate from
			// the SQLite write-lock wait this regression measures.
			warm, err := w.DB().Conn(ctx)
			must(t, err)
			must(t, warm.Close())
			released := make(chan struct{})
			go func() {
				time.Sleep(500 * time.Millisecond)
				writer.Rollback()
				close(released)
			}()
			start := time.Now()
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
			elapsed := time.Since(start)
			t.Logf("contended %s returned in %s", operation, elapsed)
			if elapsed > 150*time.Millisecond {
				t.Errorf("50ms worker operation waited for SQLite writer: %s", elapsed)
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
			must(t, w.DB().QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout))
			if timeout != 20000 {
				t.Errorf("worker leaked connection timeout into serving pool: %d", timeout)
			}
			<-released
			after, err := r.Report(ctx)
			must(t, err)
			if after != before {
				t.Errorf("contention advanced lease/watermark: before=%+v after=%+v", before, after)
			}
			resumed, err := r.Step(ctx, token, 32)
			must(t, err)
			if resumed.Processed != 32 || resumed.Generation != before.Generation {
				t.Fatal("did not resume after writer released", resumed)
			}
		})
	}
}
