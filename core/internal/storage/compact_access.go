package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"agent-nexus-core/internal/sqliteutil"
)

// initializeCompactAccessMetadata installs only empty reader capability metadata.
func (w *Workspace) initializeCompactAccessMetadata(ctx context.Context) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS resource_access_compact_control(singleton INTEGER PRIMARY KEY CHECK(singleton=1), reader INTEGER NOT NULL DEFAULT 0 CHECK(reader IN (0,1,2)), stable INTEGER NOT NULL DEFAULT 0, generation INTEGER NOT NULL DEFAULT 0, raw_cursor INTEGER, raw_done INTEGER NOT NULL DEFAULT 0, force_legacy INTEGER NOT NULL DEFAULT 0 CHECK(force_legacy IN (0,1)))`,
		`INSERT INTO resource_access_compact_control(singleton) VALUES(1) ON CONFLICT DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS resource_access_compact_edges(edge_id INTEGER PRIMARY KEY,target_key BLOB NOT NULL CHECK(typeof(target_key)='blob' AND length(target_key)=32))`,
		`CREATE INDEX IF NOT EXISTS idx_access_compact_target ON resource_access_compact_edges(target_key)`,
	} {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	// Upgrade workspaces prepared by earlier builds of this migration.
	var hasOverride bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('resource_access_compact_control') WHERE name='force_legacy')`).Scan(&hasOverride); err != nil {
		return err
	}
	if !hasOverride {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE resource_access_compact_control ADD COLUMN force_legacy INTEGER NOT NULL DEFAULT 0 CHECK(force_legacy IN (0,1))`); err != nil {
			return err
		}
	}
	if err = installAdmissionCompactSync(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// InitializeCompactAccessShadow installs transactional mirrors without copying
// history or selecting compact reads. Mirrors survive a paused worker.
func (w *Workspace) InitializeCompactAccessShadow(ctx context.Context) (err error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	defer func() {
		if err != nil && ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		}
	}()
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunkWithBusyWait(ctx, w.db, 20*time.Millisecond)
	if err != nil {
		return err
	}
	defer cleanup()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS resource_access_compact_edges (
 edge_id INTEGER PRIMARY KEY, target_key BLOB NOT NULL CHECK(typeof(target_key)='blob' AND length(target_key)=32))`,
		`CREATE INDEX IF NOT EXISTS idx_access_compact_target ON resource_access_compact_edges(target_key)`,
		`CREATE TABLE IF NOT EXISTS resource_access_compact_job (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1), cursor INTEGER, done INTEGER NOT NULL CHECK(done IN (0,1)))`,
		`INSERT INTO resource_access_compact_job VALUES(1,NULL,0) ON CONFLICT DO NOTHING`,
		// Mirror the legacy ledger, including imported updates and changed keys.
		// unhex preserves the existing digest/normalization; CHECK rejects bad keys.
		`CREATE TRIGGER IF NOT EXISTS access_compact_insert AFTER INSERT ON resource_access_exact_edges BEGIN
 INSERT INTO resource_access_compact_edges VALUES(NEW.edge_id,unhex(NEW.target_key))
 ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key; END`,
		`CREATE TRIGGER IF NOT EXISTS access_compact_update AFTER UPDATE ON resource_access_exact_edges BEGIN
 DELETE FROM resource_access_compact_edges WHERE edge_id=OLD.edge_id;
 INSERT INTO resource_access_compact_edges VALUES(NEW.edge_id,unhex(NEW.target_key))
 ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key; END`,
		`CREATE TRIGGER IF NOT EXISTS access_compact_delete AFTER DELETE ON resource_access_exact_edges BEGIN
 DELETE FROM resource_access_compact_edges WHERE edge_id=OLD.edge_id; END`,
		// This relation is for verification only. No production read selects it.
		`CREATE VIEW IF NOT EXISTS resource_access_compact_reconstructed AS
 SELECT c.edge_id,e.source_kind,e.source_id,lower(hex(c.target_key)) AS target_key
 FROM resource_access_compact_edges c JOIN resource_access_edges e ON e.rowid=c.edge_id`,
	} {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("install compact access shadow: %w", err)
		}
	}
	if err = installStableAccessEdges(ctx, tx); err != nil {
		return err
	}
	if err = installAdmissionCompactSync(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// MaintainCompactAccessBatch copies at most 200 keys under a short writer lease.
// The keyset cursor and copied rows commit together. A NULL cursor includes all
// signed SQLite rowids, including zero/negative imported keys. Triggers cover
// concurrent writes and key reuse behind the cursor.
func (w *Workspace) MaintainCompactAccessBatch(ctx context.Context) (bool, error) {
	return w.compactAccessBatch.run(ctx, 200, w.maintainCompactAccessChunk)
}

func (w *Workspace) maintainCompactAccessChunk(ctx context.Context, limit int) (bool, error) {
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunkWithBusyWait(ctx, w.db, 20*time.Millisecond)
	if err != nil {
		return false, err
	}
	defer cleanup()
	var cursor sql.NullInt64
	var done bool
	if err = tx.QueryRowContext(ctx, `SELECT cursor,done FROM resource_access_compact_job WHERE singleton=1`).Scan(&cursor, &done); err != nil {
		return false, err
	}
	if done {
		return true, tx.Commit()
	}
	// Keep the seek separate from the initial scan: a nullable OR predicate would
	// make SQLite scan the prefix on every subsequent batch.
	q := `SELECT edge_id FROM resource_access_exact_edges ORDER BY edge_id LIMIT ?`
	args := []any{limit}
	if cursor.Valid {
		q = `SELECT edge_id FROM resource_access_exact_edges WHERE edge_id>? ORDER BY edge_id LIMIT ?`
		args = []any{cursor.Int64, limit}
	}
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return false, err
	}
	var keys []int64
	for rows.Next() {
		var key int64
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return false, err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(keys) > 0 {
		cursor = sql.NullInt64{Int64: keys[len(keys)-1], Valid: true}
		if _, err = tx.ExecContext(ctx, `INSERT INTO resource_access_compact_edges
 SELECT edge_id,unhex(target_key) FROM resource_access_exact_edges WHERE edge_id BETWEEN ? AND ?
 ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key`, keys[0], cursor.Int64); err != nil {
			return false, err
		}
	} else {
		done = true
	}
	if _, err = tx.ExecContext(ctx, `UPDATE resource_access_compact_job SET cursor=?,done=? WHERE singleton=1`, cursor, done); err != nil {
		return false, err
	}
	return done, tx.Commit()
}

// CompactAccessVerification identifies one certified snapshot. Cutover rechecks
// both epoch and ledger generation under its write lease before publication.
type CompactAccessVerification struct {
	Rows           int64
	Epoch          int64
	Generation     int64
	stableVerified bool
	database       *sql.DB
}

var ErrCompactAccessMismatch = errors.New("compact access shadow differs from canonical edges; legacy reads retained")

func (w *Workspace) VerifyCompactAccessShadow(ctx context.Context) (CompactAccessVerification, error) {
	var result CompactAccessVerification
	tx, err := w.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	result, err = verifyCompactAccess(ctx, tx)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func verifyCompactAccess(ctx context.Context, tx *sql.Tx) (CompactAccessVerification, error) {
	var result CompactAccessVerification
	var err error
	var done bool
	if err = tx.QueryRowContext(ctx, `SELECT done FROM resource_access_compact_job WHERE singleton=1`).Scan(&done); err != nil {
		return result, err
	}
	if !done {
		return result, errors.New("compact access backfill is incomplete")
	}
	if err = tx.QueryRowContext(ctx, `SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&result.Epoch); err != nil {
		return result, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT generation FROM resource_access_compact_control WHERE singleton=1`).Scan(&result.Generation); err != nil {
		return result, err
	}
	// Both EXCEPT directions compare every field byte-for-byte. Also check raw
	// edges independently: an equally corrupt old/new ledger is not verification.
	legacy := `SELECT edge_id,source_kind,source_id,target_key FROM resource_access_exact_edges`
	reconstructed := `SELECT edge_id,source_kind,source_id,target_key FROM resource_access_compact_reconstructed`
	raw := `SELECT rowid,source_kind,source_id,anx_resource_atom_key(CAST(target_ref AS BLOB)) FROM resource_access_edges`
	for i, q := range []string{
		`SELECT EXISTS(` + legacy + ` EXCEPT ` + reconstructed + `)`,
		`SELECT EXISTS(` + reconstructed + ` EXCEPT ` + legacy + `)`,
		`SELECT EXISTS(` + legacy + ` EXCEPT ` + raw + `)`,
		`SELECT EXISTS(` + raw + ` EXCEPT ` + legacy + `)`,
		`SELECT EXISTS(SELECT 1 FROM resource_access_compact_edges c LEFT JOIN resource_access_edges e ON e.rowid=c.edge_id WHERE e.rowid IS NULL)`,
	} {
		var mismatch bool
		if err = tx.QueryRowContext(ctx, q).Scan(&mismatch); err != nil {
			return result, err
		}
		if mismatch {
			checks := []string{"exact_to_compact", "compact_to_exact", "exact_to_raw", "raw_to_exact", "orphan_compact_edge"}
			return result, &compactMismatchError{check: checks[i]}
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM resource_access_compact_edges`).Scan(&result.Rows); err != nil {
		return result, err
	}
	return result, nil
}

// RunCompactAccessMigration resumes bounded certification after startup.
// Verification holds a WAL read snapshot, not a writer lease.
// Reports expose one final outcome, never SQL text or resource contents.
func (w *Workspace) RunCompactAccessMigration(ctx context.Context, report func(CompactAccessVerification, error)) {
	w.runCompactAccessMigration(ctx, compactMigrationSchedule{
		budget:       10 * time.Minute,
		initialDelay: 0,
		batchDelay:   25 * time.Millisecond,
		retryMin:     250 * time.Millisecond,
		retryMax:     5 * time.Second,
	}, report)
}

// PrepareCompactAccessMigration is called before other maintenance or serving.
// It installs mirrors and certifies if possible within ten seconds. Incomplete
// durable progress resumes online; only the caller's final outcome is logged.
func (w *Workspace) PrepareCompactAccessMigration(ctx context.Context) (proof CompactAccessVerification, resume bool, err error) {
	w.runCompactAccessMigration(ctx, compactMigrationSchedule{budget: 10 * time.Second, batchDelay: 0, retryMin: 10 * time.Millisecond, retryMax: 50 * time.Millisecond}, func(p CompactAccessVerification, e error) { proof, err = p, e })
	return proof, compactMigrationRetryable(err), err
}

// A completed installation is atomic. Validate every installed object before
// resuming without a writer, including generation triggers used by strict proof
// publication. Partial/older shadows still take the normal install transaction.
func (w *Workspace) compactShadowReady(ctx context.Context) (bool, error) {
	names := []string{"resource_access_compact_job", "access_compact_insert", "access_compact_update", "access_compact_delete", "resource_access_compact_reconstructed", "resource_access_stable_edges", "access_stable_insert", "access_stable_update", "access_stable_delete"}
	for _, table := range []string{"resource_access_exact_edges", "resource_access_compact_edges", "resource_access_stable_edges"} {
		for _, op := range []string{"insert", "update", "delete"} {
			names = append(names, "compact_generation_"+table+"_"+op)
		}
	}
	args := make([]any, len(names))
	for i, name := range names {
		args[i] = name
	}
	var count int
	err := w.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name IN (`+strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")+`)`, args...).Scan(&count)
	if err != nil || count != len(names) {
		return false, err
	}
	var ready bool
	err = w.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resource_access_compact_job WHERE singleton=1)`).Scan(&ready)
	return ready, err
}

type compactObserverKey struct{}

// WithCompactAccessMigrationObserver observes completed attempts after their
// connections and transactions are released. Observers receive safe diagnostics
// and may pause between attempts; they never execute while holding a writer.
func WithCompactAccessMigrationObserver(ctx context.Context, observe func(stage string, err error)) context.Context {
	return context.WithValue(ctx, compactObserverKey{}, observe)
}
func observeCompactAttempt(ctx context.Context, stage string, err error) {
	if observe, ok := ctx.Value(compactObserverKey{}).(func(string, error)); ok {
		observe(stage, compactMigrationError(stage, err))
	}
}

// Separate timing from the worker so contention and exhaustion tests need not
// wait ten minutes. All waits happen after transaction/connection cleanup.
type compactMigrationSchedule struct {
	budget, initialDelay, batchDelay, retryMin, retryMax time.Duration
}

func compactMigrationWait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

func compactMigrationRetryable(err error) bool {
	var code interface{ Code() int }
	return errors.Is(err, ErrCompactAccessChanged) || errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &code) && (code.Code()&255 == 5 || code.Code()&255 == 6))
}

// compactBudgetCause names what consumed an exhausted budget. When the budget
// ends a bookkeeping read with nothing but the deadline, the earlier failed
// attempt (such as write contention at shadow_install) is the real cause.
func compactBudgetCause(stage string, cause error, failedStage string, failedErr error) (string, error) {
	var code interface{ Code() int }
	if failedErr != nil && errors.Is(cause, context.DeadlineExceeded) && !errors.As(cause, &code) {
		return failedStage, failedErr
	}
	return stage, cause
}

func (w *Workspace) runCompactAccessMigration(parent context.Context, schedule compactMigrationSchedule, report func(CompactAccessVerification, error)) {
	ctx, cancel := context.WithTimeout(parent, schedule.budget)
	defer cancel()
	var result CompactAccessVerification
	var lastErr error
	succeeded := false
	stage := "shadow_install"
	// The last failed attempt, kept for diagnosis if the budget then expires
	// in the next attempt's first read.
	var failedStage string
	var failedErr error
	// Exactly one final callback, including shutdown, budget exhaustion and an
	// already certified workspace. Keep the last stage/cause for diagnosis.
	defer func() {
		if !succeeded && ctx.Err() != nil {
			if parent.Err() != nil {
				lastErr = compactMigrationError(stage, parent.Err())
			} else {
				cause := lastErr
				if cause == nil {
					cause = ctx.Err()
				}
				stage, cause = compactBudgetCause(stage, cause, failedStage, failedErr)
				diagnostic := compactMigrationError(stage, cause).(*CompactAccessMigrationError)
				diagnostic.Reason = "budget_exhausted_" + diagnostic.Reason
				lastErr = diagnostic
			}
		} else {
			lastErr = compactMigrationError(stage, lastErr)
		}
		if report != nil {
			report(result, lastErr)
		}
	}()
	delay, retry := schedule.initialDelay, schedule.retryMin
	installed := false
	compactFinished := false
	for compactMigrationWait(ctx, delay) {
		if lastErr != nil {
			failedStage, failedErr = stage, lastErr
		}
		lastErr = nil
		// Inspect the durable mode before taking a writer: restarts must neither
		// reinstall a certified shadow nor undo an operator rollback.
		stage = "reader_mode"
		var mode int
		lastErr = w.db.QueryRowContext(ctx, `SELECT reader FROM resource_access_compact_control WHERE singleton=1`).Scan(&mode)
		if lastErr == nil && mode != 0 {
			succeeded = mode == 1
			if mode == 2 {
				lastErr = &CompactAccessMigrationError{Stage: stage, Reason: "operator_rollback", cause: errors.New("operator selected legacy reads")}
			}
			return
		}
		if lastErr == nil && !installed {
			stage = "shadow_state"
			installed, lastErr = w.compactShadowReady(ctx)
		}
		if lastErr == nil && !installed {
			stage = "shadow_install"
			chunkCtx, finish := context.WithTimeout(ctx, time.Second)
			lastErr = w.InitializeCompactAccessShadow(chunkCtx)
			if lastErr != nil && chunkCtx.Err() != nil {
				lastErr = errors.Join(chunkCtx.Err(), lastErr)
			}
			finish()
			installed = lastErr == nil
			observeCompactAttempt(ctx, stage, lastErr)
		}
		var done bool
		if lastErr == nil {
			stage = "compact_backfill"
			done, lastErr = w.MaintainCompactAccessBatch(ctx)
			// A committed copy is progress even if the following raw copy
			// loses the writer. Do not carry an old contention penalty forward.
			// The finishing keyset also commits copied rows; !done would skip it.
			if lastErr == nil && !compactFinished {
				retry = schedule.retryMin
			}
			compactFinished = compactFinished || (lastErr == nil && done)
		}
		if lastErr == nil {
			stage = "stable_backfill"
			rawDone, err := w.MaintainStableAccessBatch(ctx)
			done, lastErr = done && rawDone, err
			if err == nil && !rawDone {
				retry = schedule.retryMin
			}
		}
		if lastErr == nil && done {
			stage = "verification"
			verifyCtx, finish := context.WithTimeout(ctx, 5*time.Minute)
			result, lastErr = w.VerifyCompactAccessCutover(verifyCtx)
			if lastErr != nil && verifyCtx.Err() != nil {
				lastErr = errors.Join(verifyCtx.Err(), lastErr)
			}
			finish()
			observeCompactAttempt(ctx, stage, lastErr)
			if lastErr == nil {
				stage = "cutover"
				cutoverCtx, finish := context.WithTimeout(ctx, time.Second)
				lastErr = w.CutoverCompactAccess(cutoverCtx, result)
				if lastErr != nil && cutoverCtx.Err() != nil {
					lastErr = errors.Join(cutoverCtx.Err(), lastErr)
				}
				finish()
			}
			if lastErr == nil {
				succeeded = true // A committed cutover remains successful across shutdown.
				return
			}
		}
		if lastErr != nil {
			if ctx.Err() != nil || !compactMigrationRetryable(lastErr) {
				return
			}
			var code interface{ Code() int }
			if errors.As(lastErr, &code) && (code.Code()&255 == 5 || code.Code()&255 == 6) {
				// Long jitter is for actual writer contention, after releasing the lease.
				delay = retry/2 + time.Duration(rand.Int64N(int64(retry-retry/2)))
				retry = min(retry*2, schedule.retryMax)
			} else {
				// Expired adaptive slices and changed proofs retry in the next short slice.
				delay, retry = schedule.batchDelay, schedule.retryMin
			}
		} else {
			delay, retry = schedule.batchDelay, schedule.retryMin
		}
	}
}
