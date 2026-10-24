package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus-core/internal/sqliteutil"
)

// reader: 0 = preparing/legacy, 1 = certified compact, 2 = operator rollback.
// Mode 2 prevents the automatic worker from undoing an operator's rollback.
func installStableAccessEdges(ctx context.Context, tx *sql.Tx) error {
	var stable bool
	if err := tx.QueryRowContext(ctx, `SELECT stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&stable); err != nil {
		return err
	}
	if !stable {
		for _, q := range []string{
			`CREATE TABLE IF NOT EXISTS resource_access_stable_edges(edge_id INTEGER PRIMARY KEY,source_kind TEXT NOT NULL,source_id TEXT NOT NULL,target_ref TEXT NOT NULL COLLATE NOCASE,UNIQUE(source_kind,source_id,target_ref))`,
			`CREATE TRIGGER IF NOT EXISTS access_stable_insert AFTER INSERT ON resource_access_edges BEGIN INSERT INTO resource_access_stable_edges VALUES(NEW.rowid,NEW.source_kind,NEW.source_id,NEW.target_ref) ON CONFLICT(edge_id) DO UPDATE SET source_kind=excluded.source_kind,source_id=excluded.source_id,target_ref=excluded.target_ref; END`,
			`CREATE TRIGGER IF NOT EXISTS access_stable_update AFTER UPDATE ON resource_access_edges BEGIN DELETE FROM resource_access_stable_edges WHERE edge_id=OLD.rowid; INSERT INTO resource_access_stable_edges VALUES(NEW.rowid,NEW.source_kind,NEW.source_id,NEW.target_ref) ON CONFLICT(edge_id) DO UPDATE SET source_kind=excluded.source_kind,source_id=excluded.source_id,target_ref=excluded.target_ref; END`,
			`CREATE TRIGGER IF NOT EXISTS access_stable_delete AFTER DELETE ON resource_access_edges BEGIN DELETE FROM resource_access_stable_edges WHERE edge_id=OLD.rowid; END`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}
	// Bind verification to every ledger write, including direct imports that do
	// not advance the canonical authorization epoch. This is an O(1) counter.
	tables := []string{"resource_access_exact_edges", "resource_access_compact_edges"}
	if !stable {
		tables = append(tables, "resource_access_stable_edges")
	}
	for _, table := range tables {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			q := `CREATE TRIGGER IF NOT EXISTS compact_generation_` + table + `_` + strings.ToLower(op) + ` AFTER ` + op + ` ON ` + table + ` BEGIN UPDATE resource_access_compact_control SET generation=generation+1 WHERE singleton=1; END`
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *Workspace) MaintainStableAccessBatch(ctx context.Context) (bool, error) {
	return w.compactAccessBatch.run(ctx, 200, func(ctx context.Context, limit int) (bool, error) {
		tx, cleanup, err := sqliteutil.BeginMaintenanceChunkWithBusyWait(ctx, w.db, 20*time.Millisecond)
		if err != nil {
			return false, err
		}
		defer cleanup()
		var cursor sql.NullInt64
		var done, stable bool
		if err = tx.QueryRowContext(ctx, `SELECT raw_cursor,raw_done,stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&cursor, &done, &stable); err != nil {
			return false, err
		}
		if done || stable {
			return true, tx.Commit()
		}
		q, args := `SELECT rowid FROM resource_access_edges ORDER BY rowid LIMIT ?`, []any{limit}
		if cursor.Valid {
			q, args = `SELECT rowid FROM resource_access_edges WHERE rowid>? ORDER BY rowid LIMIT ?`, []any{cursor.Int64, limit}
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return false, err
		}
		var keys []int64
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return false, err
			}
			keys = append(keys, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
		if len(keys) > 0 {
			cursor = sql.NullInt64{Int64: keys[len(keys)-1], Valid: true}
			if _, err = tx.ExecContext(ctx, `INSERT INTO resource_access_stable_edges SELECT rowid,source_kind,source_id,target_ref FROM resource_access_edges WHERE rowid BETWEEN ? AND ? ON CONFLICT(edge_id) DO UPDATE SET source_kind=excluded.source_kind,source_id=excluded.source_id,target_ref=excluded.target_ref`, keys[0], cursor.Int64); err != nil {
				return false, err
			}
		} else {
			done = true
		}
		if _, err = tx.ExecContext(ctx, `UPDATE resource_access_compact_control SET raw_cursor=?,raw_done=? WHERE singleton=1`, cursor, done); err != nil {
			return false, err
		}
		return done, tx.Commit()
	})
}

// VerifyCompactAccessCutover certifies both ledgers and stable raw identities in
// one snapshot. It never grants a cached or process-local reader capability.
func (w *Workspace) VerifyCompactAccessCutover(ctx context.Context) (CompactAccessVerification, error) {
	// Reuse verification through a pinned one-connection snapshot by factoring
	// its SQL into the shared verifier below.
	tx, err := w.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CompactAccessVerification{}, err
	}
	defer tx.Rollback()
	result, err := verifyCompactAccess(ctx, tx)
	if err != nil {
		return result, err
	}
	var stable, done bool
	if err = tx.QueryRowContext(ctx, `SELECT stable,raw_done FROM resource_access_compact_control WHERE singleton=1`).Scan(&stable, &done); err != nil {
		return result, err
	}
	if !stable {
		if !done {
			return result, errors.New("stable raw-edge backfill is incomplete")
		}
		old := `SELECT rowid,source_kind,source_id,CAST(target_ref AS BLOB) FROM resource_access_edges`
		newRows := `SELECT edge_id,source_kind,source_id,CAST(target_ref AS BLOB) FROM resource_access_stable_edges`
		for _, q := range []string{`SELECT EXISTS(` + old + ` EXCEPT ` + newRows + `)`, `SELECT EXISTS(` + newRows + ` EXCEPT ` + old + `)`} {
			var mismatch bool
			if err = tx.QueryRowContext(ctx, q).Scan(&mismatch); err != nil {
				return result, err
			}
			if mismatch {
				return result, &compactMismatchError{check: "stable_raw_identity"}
			}
		}
	}
	result.stableVerified, result.database = true, w.db
	return result, tx.Commit()
}

var ErrCompactAccessChanged = errors.New("access ledgers changed after verification; cutover deferred")

// CutoverCompactAccess requires a fresh verification snapshot. Its write lease
// checks epoch AND ledger generation before publishing the new format and mode.
// A successful publication also invalidates all existing authorization caches.
// The caller's deadline bounds the cutover; without one it gets one second.
func (w *Workspace) CutoverCompactAccess(ctx context.Context, proof CompactAccessVerification) (err error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Second)
		defer cancel()
	}
	defer func() {
		if err != nil && ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		}
	}()
	if !proof.stableVerified || proof.database != w.db {
		return errors.New("cutover requires this workspace's complete certification")
	}
	if w.processLock == nil {
		return errors.New("cutover requires a serving workspace lease")
	}
	if err := w.processLock.activate(filepath.Join(w.layout.RootDir, ".scope-serving.lock")); err != nil {
		return err
	}
	conn, err := w.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Discard this maintenance connection on exit: never leak its nonblocking
	// busy handler or legacy rename setting into serving requests.
	defer conn.Raw(func(any) error { return driver.ErrBadConn })
	if _, err = conn.ExecContext(ctx, `PRAGMA busy_timeout=20`); err != nil {
		return err
	}
	// The rename must leave cross-table trigger bodies and views pointing at the
	// public table name.
	if _, err = conn.ExecContext(ctx, `PRAGMA legacy_alter_table=ON`); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var epoch, generation int64
	var stable, done bool
	var reader int
	if err = tx.QueryRowContext(ctx, `SELECT e.version,c.generation,c.stable,c.raw_done,c.reader FROM resource_access_epoch e,resource_access_compact_control c WHERE e.singleton=1 AND c.singleton=1`).Scan(&epoch, &generation, &stable, &done, &reader); err != nil {
		return err
	}
	if reader == 2 {
		return errors.New("operator selected legacy reads")
	}
	if epoch != proof.Epoch || generation != proof.Generation || (!stable && !done) {
		return ErrCompactAccessChanged
	}
	if !stable {
		// Capture before any rename. Source writers use explicit columns so the
		// added INTEGER PRIMARY KEY does not alter their INSERT contracts.
		rows, err := tx.QueryContext(ctx, `SELECT name,sql FROM sqlite_schema WHERE type='trigger' AND (tbl_name='resource_access_edges' OR sql LIKE '%INSERT INTO resource_access_edges VALUES%' OR sql LIKE '%INSERT INTO resource_access_edges SELECT%') ORDER BY name`)
		if err != nil {
			return err
		}
		type trigger struct{ name, sql string }
		var triggers []trigger
		for rows.Next() {
			var tr trigger
			if err = rows.Scan(&tr.name, &tr.sql); err != nil {
				rows.Close()
				return err
			}
			if !strings.HasPrefix(tr.name, "access_stable_") {
				triggers = append(triggers, tr)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, name := range []string{"access_stable_insert", "access_stable_update", "access_stable_delete"} {
			if _, err = tx.ExecContext(ctx, `DROP TRIGGER `+name); err != nil {
				return err
			}
		}
		for _, tr := range triggers {
			if _, err = tx.ExecContext(ctx, `DROP TRIGGER "`+strings.ReplaceAll(tr.name, `"`, `""`)+`"`); err != nil {
				return err
			}
		}
		for _, q := range []string{
			`ALTER TABLE resource_access_edges RENAME TO resource_access_unstable_edges`,
			`ALTER TABLE resource_access_stable_edges RENAME TO resource_access_edges`,
		} {
			if _, err = tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("stabilize access identities: %w", err)
			}
		}
		for _, tr := range triggers {
			q := strings.ReplaceAll(tr.sql, "INSERT INTO resource_access_edges VALUES", "INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES")
			q = strings.ReplaceAll(q, "INSERT INTO resource_access_edges SELECT", "INSERT INTO resource_access_edges(source_kind,source_id,target_ref) SELECT")
			if tr.name == "access_mention_edge_insert" || tr.name == "access_mention_edge_update" {
				// SQLite REPLACE may delete a colliding raw row without firing
				// its DELETE trigger. Repair only the indexed conflicting tuple
				// and NEW key before rebuilding derived edges; no corpus scan.
				orphan := `SELECT x.edge_id FROM resource_access_exact_edges x WHERE x.target_key=anx_resource_atom_key(CAST(NEW.target_ref AS BLOB)) AND x.source_kind=NEW.source_kind AND x.source_id=NEW.source_id AND x.edge_id<>NEW.rowid AND NOT EXISTS(SELECT 1 FROM resource_access_edges r WHERE r.rowid=x.edge_id)`
				repair := `DELETE FROM resource_access_mentions WHERE edge_id IN (` + orphan + `); DELETE FROM resource_access_mention_buckets WHERE edge_id IN (` + orphan + `); DELETE FROM resource_access_exact_edges WHERE edge_id IN (` + orphan + `); DELETE FROM resource_access_mentions WHERE edge_id=NEW.rowid; DELETE FROM resource_access_mention_buckets WHERE edge_id=NEW.rowid; DELETE FROM resource_access_exact_edges WHERE edge_id=NEW.rowid; `
				if !strings.Contains(q, " BEGIN ") {
					return errors.New("unrecognized access edge trigger; cutover refused")
				}
				q = strings.Replace(q, " BEGIN ", " BEGIN "+repair, 1)
			}
			if _, err = tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE resource_access_compact_control SET reader=1,stable=1 WHERE singleton=1; UPDATE resource_access_epoch SET version=version+1 WHERE singleton=1`); err != nil {
		return err
	}
	return tx.Commit()
}

func (w *Workspace) RollbackCompactAccess(ctx context.Context) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE resource_access_compact_control SET reader=2 WHERE singleton=1; UPDATE resource_access_epoch SET version=version+1 WHERE singleton=1`); err != nil {
		return err
	}
	return tx.Commit()
}

func enableCompactAccessLease(ctx context.Context, db *sql.DB, lease *scopeProcessLease, root string) error {
	var exists, stable bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='resource_access_compact_control')`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := db.QueryRowContext(ctx, `SELECT stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&stable); err != nil {
		return err
	}
	if stable {
		return lease.activate(filepath.Join(root, ".scope-serving.lock"))
	}
	return nil
}

// SetCompactAccessLegacyReads applies the process configuration before serving.
// It neither grants certification nor overrides a durable operator rollback.
// The epoch bump makes every consuming authorization snapshot see the switch.
func (w *Workspace) SetCompactAccessLegacyReads(ctx context.Context, force bool) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE resource_access_compact_control SET force_legacy=? WHERE singleton=1 AND force_legacy<>?`, force, force)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE resource_access_epoch SET version=version+1 WHERE singleton=1`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
