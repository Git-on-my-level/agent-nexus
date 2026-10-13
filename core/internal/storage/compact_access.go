package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"agent-nexus-core/internal/sqliteutil"
)

// InitializeCompactAccessShadow opts this database into the expand-only shadow
// experiment. It does no historical copying. The old ledger remains authoritative
// and all installed triggers persist even when the maintenance worker is paused.
func (w *Workspace) InitializeCompactAccessShadow(ctx context.Context) error {
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunk(ctx, w.db)
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
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunk(ctx, w.db)
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

// CompactAccessVerification describes ONE read snapshot, never a cutover token.
// Subsequent writes can invalidate the result. A future reader switch needs its
// own transactional capability/epoch handshake and fanout performance gate.
type CompactAccessVerification struct {
	Rows  int64
	Epoch int64
}

var ErrCompactAccessMismatch = errors.New("compact access shadow differs from canonical edges; legacy reads retained")

func (w *Workspace) VerifyCompactAccessShadow(ctx context.Context) (CompactAccessVerification, error) {
	var result CompactAccessVerification
	tx, err := w.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
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
	// Both EXCEPT directions compare every field byte-for-byte. Also check raw
	// edges independently: an equally corrupt old/new ledger is not verification.
	legacy := `SELECT edge_id,source_kind,source_id,target_key FROM resource_access_exact_edges`
	reconstructed := `SELECT edge_id,source_kind,source_id,target_key FROM resource_access_compact_reconstructed`
	raw := `SELECT rowid,source_kind,source_id,anx_resource_atom_key(CAST(target_ref AS BLOB)) FROM resource_access_edges`
	for _, q := range []string{
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
			return result, ErrCompactAccessMismatch
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM resource_access_compact_edges`).Scan(&result.Rows); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

// RunCompactAccessShadow is opt-in server maintenance, never startup readiness
// or request work. Verification holds a WAL read snapshot, not a writer lease.
// Errors contain no resource contents; callers should log only generic status.
func (w *Workspace) RunCompactAccessShadow(ctx context.Context, report func(CompactAccessVerification, error)) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	installed := false
	var lastReport time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		var err error
		if !installed {
			chunkCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			err = w.InitializeCompactAccessShadow(chunkCtx)
			cancel()
			installed = err == nil
		}
		var done bool
		if err == nil {
			done, err = w.MaintainCompactAccessBatch(ctx)
		}
		if err != nil {
			if report != nil && ctx.Err() == nil && time.Since(lastReport) >= time.Minute {
				report(CompactAccessVerification{}, err)
				lastReport = time.Now()
			}
			continue
		}
		if done {
			verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			result, err := w.VerifyCompactAccessShadow(verifyCtx)
			cancel()
			if report != nil && ctx.Err() == nil {
				report(result, err)
			}
			return // A mismatch requires operator investigation, not endless rescans.
		}
	}
}
