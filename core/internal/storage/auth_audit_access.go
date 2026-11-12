package storage

import (
	"agent-nexus-core/internal/loopstats"
	"agent-nexus-core/internal/sqliteutil"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Auth audit rows are auth-admin-only at the repository read. Their metadata no
// longer contributes filter-source access edges, so the old per-row triggers
// are dropped and existing edges are removed by a bounded, resumable job. The
// migration itself never deletes edges: startup stays short and each committed
// chunk cascades through exact/compact/admission indexes atomically.
const (
	authAuditAccessJobTable   = "resource_access_auth_audit_job"
	authAuditAccessSourceKind = "filter/auth_audit_events"
	authAuditAccessBatchSize  = 2000
)

func installAuthAuditAccessCleanup(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`DROP TRIGGER IF EXISTS access_auth_audit_events_insert`,
		`DROP TRIGGER IF EXISTS access_auth_audit_events_update`,
		`DROP TRIGGER IF EXISTS access_auth_audit_events_delete`,
		// Scope-inbox invalidation is not wired in production, but a preview
		// installation may have left audit rows in its dependency ledger.
		`DROP TRIGGER IF EXISTS scope_inbox_invalidate_auth_audit_events_insert`,
		`DROP TRIGGER IF EXISTS scope_inbox_invalidate_auth_audit_events_update`,
		`DROP TRIGGER IF EXISTS scope_inbox_invalidate_auth_audit_events_delete`,
		`CREATE TABLE IF NOT EXISTS ` + authAuditAccessJobTable + `(singleton INTEGER PRIMARY KEY CHECK(singleton=1),cursor INTEGER,done INTEGER NOT NULL DEFAULT 0 CHECK(done IN (0,1)))`,
		`INSERT INTO ` + authAuditAccessJobTable + `(singleton,cursor,done) VALUES(1,NULL,0) ON CONFLICT DO NOTHING`,
		`UPDATE ` + authAuditAccessJobTable + ` SET cursor=NULL,done=0 WHERE singleton=1`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("install auth audit access cleanup: %w", err)
		}
	}
	return nil
}

// MaintainAuthAuditAccessBatch deletes at most one bounded page of legacy
// filter/auth_audit_events edges. The cursor and deleted rows commit together,
// so an interrupted run resumes without skipping or redoing committed work.
func (w *Workspace) MaintainAuthAuditAccessBatch(ctx context.Context) (bool, error) {
	ctx, finish := loopstats.Start(ctx, "auth_audit_access")
	defer finish()
	return w.maintainAuthAuditAccessChunk(ctx, authAuditAccessBatchSize)
}

func (w *Workspace) maintainAuthAuditAccessChunk(ctx context.Context, limit int) (bool, error) {
	sqlCtx := context.WithoutCancel(ctx)
	var exists bool
	err := w.db.QueryRowContext(sqlCtx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='`+authAuditAccessJobTable+`')`).Scan(&exists)
	if err != nil || !exists {
		return true, err
	}
	var idle bool
	if err = w.db.QueryRowContext(sqlCtx, `SELECT done FROM `+authAuditAccessJobTable+` WHERE singleton=1`).Scan(&idle); err != nil || idle {
		return idle, err
	}
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunkWithBusyWait(sqlCtx, w.db, 20*time.Millisecond)
	if err != nil {
		return false, err
	}
	defer cleanup()
	done, err := deleteAuthAuditAccessEdges(sqlCtx, tx, limit)
	if err != nil {
		return false, err
	}
	return done, tx.Commit()
}

func deleteAuthAuditAccessEdges(ctx context.Context, tx *sql.Tx, limit int) (bool, error) {
	limit = max(1, limit)
	var cursor sql.NullInt64
	var done bool
	if err := tx.QueryRowContext(ctx, `SELECT cursor,done FROM `+authAuditAccessJobTable+` WHERE singleton=1`).Scan(&cursor, &done); err != nil || done {
		return done, err
	}
	q := `SELECT rowid FROM resource_access_edges WHERE source_kind=? ORDER BY rowid LIMIT ?`
	args := []any{authAuditAccessSourceKind, limit}
	if cursor.Valid {
		q = `SELECT rowid FROM resource_access_edges WHERE source_kind=? AND rowid>? ORDER BY rowid LIMIT ?`
		args = []any{authAuditAccessSourceKind, cursor.Int64, limit}
	}
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return false, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	loopstats.Rows(ctx, len(ids))
	if len(ids) > 0 {
		cursor = sql.NullInt64{Int64: ids[len(ids)-1], Valid: true}
		encoded, err := json.Marshal(ids)
		if err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM resource_access_edges WHERE rowid IN (SELECT value FROM json_each(?))`, string(encoded)); err != nil {
			return false, err
		}
	}
	// A short page means the cursor reached the end; no extra empty chunk is
	// needed to publish completion.
	done = len(ids) < limit
	if _, err = tx.ExecContext(ctx, `UPDATE `+authAuditAccessJobTable+` SET cursor=?,done=? WHERE singleton=1`, cursor, done); err != nil {
		return false, err
	}
	return done, nil
}

// RunAuthAuditAccessMaintenance drains legacy audit filter edges after startup.
// Failures back off exponentially; leaving an edge in place is authorization-safe
// because filter sources never match a real resource.
func (w *Workspace) RunAuthAuditAccessMaintenance(ctx context.Context, report func(error)) {
	delay, failures := 50*time.Millisecond, 0
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		done, err := w.MaintainAuthAuditAccessBatch(ctx)
		if err == nil {
			if done {
				return
			}
			delay, failures = 50*time.Millisecond, 0
			timer.Reset(delay)
			continue
		}
		if ctx.Err() != nil {
			return
		}
		failures++
		delay = 50 * time.Millisecond
		for i := 1; i < failures && delay < 5*time.Minute; i++ {
			delay *= 2
		}
		delay = min(delay, 5*time.Minute)
		if report != nil {
			report(fmt.Errorf("auth audit access cleanup (%s), retry %d in %s: %w", maintenanceErrorClass(err), failures, delay, err))
		}
		timer.Reset(delay)
	}
}
