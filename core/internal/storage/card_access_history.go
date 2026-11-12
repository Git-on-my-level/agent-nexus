package storage

// Card revisions are immutable, but each one used to keep its own copy of
// every reference edge (revision row plus revision artifact), so the access
// graph grew with revisions x refs. A card, its revisions and their artifacts
// already share one denial outcome: an artifact or revision denial propagates
// to its card, and a card denial propagates back to all of its revisions and
// artifacts. Keying those reference edges by the card keeps the same union of
// targets, and therefore the same authorization result, while storing each
// distinct target once per card.
//
// Only an artifact owned by exactly one card (and no document revision) moves
// into that card's set; shared document/card artifacts keep their own edges.
// Writers are unchanged: triggers redirect revision and exclusive revision
// artifact edge inserts into the card's set, and a new revision row moves its
// artifact's edges (written before the revision existed) into that set.
//
// The set only grows on ordinary writes. When a direct write changes or
// removes a revision's reference-bearing fields, the affected cards' sets are
// rebuilt from the canonical rows. Deleting a revision leaves its targets in
// the card's set (more restrictive, never less) until the card and all of its
// revisions are gone; purge removes both.

import (
	"agent-nexus-core/internal/loopstats"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/sqliteutil"
)

const cardHistoryJobTable = "resource_access_card_history_job"

func installCardAccessHistory(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"resource_access_edges", "card_revisions", "artifacts", "cards"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !exists {
			return err
		}
	}
	documents, err := sqliteTableExists(ctx, tx, "document_revisions")
	if err != nil {
		return err
	}
	kind := "'" + resourceaccess.CardHistoryKind + "'"
	// Only an artifact owned by exactly one card (and no document revision)
	// moves into that card's set; shared artifacts keep their own edges.
	unshared := func(artifact string) string {
		single := "(SELECT count(DISTINCT card_id) FROM card_revisions WHERE artifact_id=" + artifact + ")=1"
		if !documents {
			return single
		}
		return single + " AND NOT EXISTS(SELECT 1 FROM document_revisions d WHERE d.artifact_id=" + artifact + ")"
	}
	sharedWithOther := func(artifact string) string {
		out := "(SELECT count(DISTINCT card_id) FROM card_revisions WHERE artifact_id=" + artifact + ")<>1"
		if documents {
			out += " OR EXISTS(SELECT 1 FROM document_revisions d WHERE d.artifact_id=" + artifact + ")"
		}
		return "(" + out + ")"
	}
	// Regenerate edges through the existing per-table triggers, which the
	// redirect then routes into each owning card's set. These self assignments
	// fire AFTER UPDATE OF triggers without changing any value.
	rebuild := func(cards string) string {
		return `DELETE FROM resource_access_edges WHERE source_kind=` + kind + ` AND source_id IN (` + cards + `);` +
			` UPDATE artifacts SET refs_json=refs_json WHERE id IN (SELECT artifact_id FROM card_revisions WHERE card_id IN (` + cards + `));` +
			` UPDATE card_revisions SET refs_json=refs_json WHERE card_id IN (` + cards + `);`
	}
	move := func(sourceKind, sourceID, card, where string) string {
		return `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) SELECT ` + kind + `,` + card + `,e.target_ref FROM resource_access_edges e WHERE e.source_kind='` + sourceKind + `' AND e.source_id=` + sourceID + ` AND ` + where + ` ON CONFLICT(source_kind,source_id,target_ref) DO NOTHING;` +
			` DELETE FROM resource_access_edges WHERE source_kind='` + sourceKind + `' AND source_id=` + sourceID + ` AND ` + where + `;`
	}
	removeOrphanSet := func(card string) string {
		return `DELETE FROM resource_access_edges WHERE source_kind=` + kind + ` AND source_id=` + card + ` AND NOT EXISTS(SELECT 1 FROM cards WHERE id=` + card + `) AND NOT EXISTS(SELECT 1 FROM card_revisions WHERE card_id=` + card + `);`
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS ` + cardHistoryJobTable + `(singleton INTEGER PRIMARY KEY CHECK(singleton=1),cursor TEXT NOT NULL DEFAULT '',done INTEGER NOT NULL DEFAULT 0 CHECK(done IN (0,1)))`,
		`INSERT INTO ` + cardHistoryJobTable + `(singleton) VALUES(1) ON CONFLICT DO NOTHING`,
		`DROP TRIGGER IF EXISTS access_card_history_artifact_redirect`,
		`DROP TRIGGER IF EXISTS access_card_history_revision_redirect`,
		`DROP TRIGGER IF EXISTS access_card_history_revision_insert`,
		`DROP TRIGGER IF EXISTS access_card_history_revision_update`,
		`DROP TRIGGER IF EXISTS access_card_history_revision_delete`,
		`DROP TRIGGER IF EXISTS access_card_history_artifact_update`,
		`DROP TRIGGER IF EXISTS access_card_history_card_delete`,
		`DROP TRIGGER IF EXISTS access_card_history_document_revision_insert`,
		`DROP TRIGGER IF EXISTS access_card_history_document_revision_update`,
		// RAISE(IGNORE) skips only this row; the redirected row fires the
		// usual mirror, mention and epoch triggers.
		`CREATE TRIGGER access_card_history_artifact_redirect BEFORE INSERT ON resource_access_edges WHEN NEW.source_kind='artifact' AND ` + unshared("NEW.source_id") + ` BEGIN
 INSERT INTO resource_access_edges(source_kind,source_id,target_ref) SELECT DISTINCT ` + kind + `,r.card_id,NEW.target_ref FROM card_revisions r WHERE r.artifact_id=NEW.source_id ON CONFLICT(source_kind,source_id,target_ref) DO NOTHING;
 SELECT RAISE(IGNORE); END`,
		`CREATE TRIGGER access_card_history_revision_redirect BEFORE INSERT ON resource_access_edges WHEN NEW.source_kind='card_revision' AND EXISTS(SELECT 1 FROM card_revisions r WHERE r.revision_id=NEW.source_id) BEGIN
 INSERT INTO resource_access_edges(source_kind,source_id,target_ref) SELECT DISTINCT ` + kind + `,r.card_id,NEW.target_ref FROM card_revisions r WHERE r.revision_id=NEW.source_id ON CONFLICT(source_kind,source_id,target_ref) DO NOTHING;
 SELECT RAISE(IGNORE); END`,
		// The revision artifact is written before its revision row. An
		// artifact that this row makes shared regains its own edges.
		`CREATE TRIGGER access_card_history_revision_insert AFTER INSERT ON card_revisions BEGIN ` +
			move("artifact", "NEW.artifact_id", "NEW.card_id", unshared("NEW.artifact_id")) +
			move("card_revision", "NEW.revision_id", "NEW.card_id", "1") +
			` UPDATE artifacts SET refs_json=refs_json WHERE id=NEW.artifact_id AND ` + sharedWithOther("NEW.artifact_id") + `; END`,
		`CREATE TRIGGER access_card_history_revision_update AFTER UPDATE OF revision_id,card_id,artifact_id,refs_json,prev_revision_id ON card_revisions WHEN OLD.revision_id IS NOT NEW.revision_id OR OLD.card_id IS NOT NEW.card_id OR OLD.artifact_id IS NOT NEW.artifact_id OR OLD.refs_json IS NOT NEW.refs_json OR OLD.prev_revision_id IS NOT NEW.prev_revision_id BEGIN ` +
			rebuild("OLD.card_id,NEW.card_id") +
			// A detached or newly shared artifact regains its own edges.
			` UPDATE artifacts SET refs_json=refs_json WHERE id IN (OLD.artifact_id,NEW.artifact_id);` +
			removeOrphanSet("OLD.card_id") + ` END`,
		`CREATE TRIGGER access_card_history_revision_delete AFTER DELETE ON card_revisions BEGIN
 UPDATE artifacts SET refs_json=refs_json WHERE id=OLD.artifact_id AND NOT EXISTS(SELECT 1 FROM card_revisions WHERE artifact_id=OLD.artifact_id); ` +
			removeOrphanSet("OLD.card_id") + ` END`,
		// Owned artifacts reject lifecycle writes, and indexing a NULL content
		// manifest or adding a trash reason only adds targets. Rebuild when a
		// direct write could remove one.
		`CREATE TRIGGER access_card_history_artifact_update AFTER UPDATE OF id,refs_json,metadata_json,content_refs_json,trash_reason ON artifacts WHEN (OLD.id IS NOT NEW.id OR OLD.refs_json IS NOT NEW.refs_json OR OLD.metadata_json IS NOT NEW.metadata_json OR (OLD.content_refs_json IS NOT NULL AND OLD.content_refs_json IS NOT NEW.content_refs_json) OR (OLD.trash_reason IS NOT NULL AND OLD.trash_reason IS NOT NEW.trash_reason)) AND EXISTS(SELECT 1 FROM card_revisions WHERE artifact_id IN (OLD.id,NEW.id)) BEGIN ` +
			rebuild("SELECT card_id FROM card_revisions WHERE artifact_id IN (OLD.id,NEW.id)") + ` END`,
		`CREATE TRIGGER access_card_history_card_delete AFTER DELETE ON cards BEGIN ` + removeOrphanSet("OLD.id") + ` END`,
	}
	if documents {
		// An artifact that starts backing a document revision regains its own
		// edges so the document's denial still follows the artifact's targets,
		// while the card's retained set stays conservative.
		regain := ` UPDATE artifacts SET refs_json=refs_json WHERE id=NEW.artifact_id AND EXISTS(SELECT 1 FROM card_revisions WHERE artifact_id=NEW.artifact_id); END`
		statements = append(statements,
			`CREATE TRIGGER access_card_history_document_revision_insert AFTER INSERT ON document_revisions BEGIN`+regain,
			`CREATE TRIGGER access_card_history_document_revision_update AFTER UPDATE OF artifact_id ON document_revisions WHEN OLD.artifact_id IS NOT NEW.artifact_id BEGIN`+regain,
		)
	}
	statements = append(statements,
		// Restart the bounded backfill whenever this installer runs.
		`UPDATE `+cardHistoryJobTable+` SET cursor='',done=0 WHERE singleton=1`,
	)
	for _, q := range statements {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	// Migration 143 runs after admission was installed, so its trigger bodies
	// predate card_history. Install the one relationship those bodies need now;
	// fresh workspaces already created it in migration 116 and this is a no-op.
	if exists, err := sqliteTableExists(ctx, tx, "resource_access_admission_links"); err != nil {
		return err
	} else if exists {
		for _, name := range []string{"admission_card_history_insert", "admission_card_history_update", "admission_card_history_delete"} {
			if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS `+name); err != nil {
				return err
			}
		}
		link := `INSERT OR IGNORE INTO resource_access_admission_links(origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger) SELECT 'card_history',`
		if _, err := tx.ExecContext(ctx, link+`id,0,'card_history',id,'card',id,0 FROM cards WHERE true`); err != nil {
			return err
		}
		for _, q := range []string{
			`CREATE TRIGGER admission_card_history_insert AFTER INSERT ON cards BEGIN ` + link + `NEW.id,0,'card_history',NEW.id,'card',NEW.id,0 WHERE true; END`,
			`CREATE TRIGGER admission_card_history_update AFTER UPDATE OF id ON cards WHEN OLD.id IS NOT NEW.id BEGIN DELETE FROM resource_access_admission_links WHERE origin='card_history' AND origin_id=CAST(OLD.id AS TEXT); ` + link + `NEW.id,0,'card_history',NEW.id,'card',NEW.id,0 WHERE true; END`,
			`CREATE TRIGGER admission_card_history_delete AFTER DELETE ON cards BEGIN DELETE FROM resource_access_admission_links WHERE origin='card_history' AND origin_id=CAST(OLD.id AS TEXT); END`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}
	return nil
}

// MaintainCardAccessHistoryBatch moves a bounded number of legacy per-revision
// edges into their card's set in one short transaction. Each committed chunk
// preserves authorization; the cursor makes it resumable.
func (w *Workspace) MaintainCardAccessHistoryBatch(ctx context.Context) (bool, error) {
	ctx, finish := loopstats.Start(ctx, "card_history")
	defer finish()
	return w.maintainCardAccessHistoryChunk(ctx, 1000, cardHistoryChunkBudget)
}

// cardHistoryChunkBudget bounds one chunk's write transaction. It is enforced
// between statements, never by cancelling a statement.
const cardHistoryChunkBudget = 50 * time.Millisecond

// cardHistoryEdgeBatch is the most edges one statement pair moves.
const cardHistoryEdgeBatch = 25

func (w *Workspace) maintainCardAccessHistoryChunk(ctx context.Context, limit int, budget time.Duration) (bool, error) {
	sqlCtx := context.WithoutCancel(ctx)
	var exists bool
	err := w.db.QueryRowContext(sqlCtx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='`+cardHistoryJobTable+`')`).Scan(&exists)
	if err != nil || !exists {
		return true, err
	}
	var idle bool
	if err = w.db.QueryRowContext(sqlCtx, `SELECT done FROM `+cardHistoryJobTable+` WHERE singleton=1`).Scan(&idle); err != nil || idle {
		return idle, err
	}

	// Cancelling a statement inside this write transaction can leave the
	// driver connection open outside the pool with the write lock held, which
	// blocks every later writer until the process exits. Statements therefore
	// run uncancelled; the time budget and shutdown are checked between them.
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunkWithBusyWait(sqlCtx, w.db, 20*time.Millisecond)
	if err != nil {
		return false, err
	}
	defer cleanup()
	deadline := time.Now().Add(budget)
	done, err := moveCardAccessHistoryUntil(sqlCtx, tx, limit, func() bool { return ctx.Err() != nil || time.Now().After(deadline) })
	if err != nil {
		return false, err
	}
	return done, tx.Commit()
}

// moveCardAccessHistory moves up to limit edges and visits at most 4*limit
// revisions. A revision is committed to the cursor only once it has no own
// edges left, so a partial revision resumes on the next chunk.
func moveCardAccessHistory(ctx context.Context, tx *sql.Tx, limit int) (bool, error) {
	return moveCardAccessHistoryUntil(ctx, tx, limit, func() bool { return false })
}

// moveCardAccessHistoryUntil also stops, keeping committed-cursor semantics,
// once stop reports true after at least one statement pair.
func moveCardAccessHistoryUntil(ctx context.Context, tx *sql.Tx, limit int, stop func() bool) (bool, error) {
	limit = max(1, limit)
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='`+cardHistoryJobTable+`')`).Scan(&exists); err != nil || !exists {
		return true, err
	}
	var cursor string
	var done bool
	if err := tx.QueryRowContext(ctx, `SELECT cursor,done FROM `+cardHistoryJobTable+` WHERE singleton=1`).Scan(&cursor, &done); err != nil || done {
		return done, err
	}
	documents, err := sqliteTableExists(ctx, tx, "document_revisions")
	if err != nil {
		return false, err
	}
	// Shared artifacts keep their own edges (see installCardAccessHistory).
	shared := `SELECT (SELECT count(DISTINCT card_id) FROM card_revisions WHERE artifact_id=?1)<>1`
	if documents {
		shared += ` OR EXISTS(SELECT 1 FROM document_revisions WHERE artifact_id=?1)`
	}
	budget, visited, progressed := limit, 0, false
	for budget > 0 && visited < 4*limit {
		var revision, card, artifact string
		err := tx.QueryRowContext(ctx, `SELECT revision_id,card_id,artifact_id FROM card_revisions WHERE revision_id>? ORDER BY revision_id LIMIT 1`, cursor).Scan(&revision, &card, &artifact)
		if err == sql.ErrNoRows {
			done = true
			break
		}
		if err != nil {
			return false, err
		}
		visited++
		loopstats.Rows(ctx, 1)
		sources := [][2]string{{"card_revision", revision}}
		var isShared bool
		if err = tx.QueryRowContext(ctx, shared, artifact).Scan(&isShared); err != nil {
			return false, err
		}
		if !isShared {
			sources = append(sources, [2]string{"artifact", artifact})
		}
		complete := true
	sources:
		for i, source := range sources {
			if i > 0 && progressed && stop() {
				// Out of budget between this revision's sources: leave the
				// cursor on it; the next chunk finishes the remaining source.
				complete = false
				break
			}
			for {
				request := min(budget, cardHistoryEdgeBatch)
				moved, err := moveCardAccessEdges(ctx, tx, source[0], source[1], card, request)
				if err != nil {
					return false, err
				}
				budget -= moved
				progressed = progressed || moved > 0
				if moved < request {
					break // this source has no own edges left
				}
				if budget <= 0 || stop() {
					// Re-check on the next chunk; there may be nothing left.
					complete = false
					break sources
				}
			}
		}
		if !complete {
			break
		}
		cursor = revision
		if stop() {
			break
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+cardHistoryJobTable+` SET cursor=?,done=? WHERE singleton=1`, cursor, done); err != nil {
		return false, err
	}
	return done, nil
}

func moveCardAccessEdges(ctx context.Context, tx *sql.Tx, kind, id, card string, limit int) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT rowid FROM resource_access_edges WHERE source_kind=? AND source_id=? LIMIT ?`, kind, id, limit)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var rowid int64
		if err = rows.Scan(&rowid); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, rowid)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return 0, err
	}
	// Insert before delete in one transaction: no committed state lacks a target.
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) SELECT ?,?,target_ref FROM resource_access_edges WHERE rowid IN (SELECT value FROM json_each(?)) ON CONFLICT(source_kind,source_id,target_ref) DO NOTHING`, resourceaccess.CardHistoryKind, card, string(encoded)); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM resource_access_edges WHERE rowid IN (SELECT value FROM json_each(?))`, string(encoded)); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// RunCardAccessHistoryMaintenance drains the legacy per-revision edges after
// startup. Freed pages are reused by later writes; shrinking the file is a
// separate offline step (see the runbook).
//
// Failures back off exponentially (capped at five minutes) and report the
// revision the cursor is waiting on. A revision is never skipped: leaving its
// edges in place is always authorization-safe, so a persistent failure keeps
// blocking the job cheaply instead of spinning.
func (w *Workspace) RunCardAccessHistoryMaintenance(ctx context.Context, report func(error)) {
	w.runCardAccessHistoryMaintenance(ctx, report, cardHistoryRetry{base: 50 * time.Millisecond, max: 5 * time.Minute, stuckAfter: 10})
}

type cardHistoryRetry struct {
	base, max  time.Duration
	stuckAfter int
}

func (w *Workspace) runCardAccessHistoryMaintenance(ctx context.Context, report func(error), retry cardHistoryRetry) {
	delay, failures := retry.base, 0
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		done, err := w.MaintainCardAccessHistoryBatch(ctx)
		if err == nil {
			if done {
				return
			}
			delay, failures = retry.base, 0
			timer.Reset(delay)
			continue
		}
		if ctx.Err() != nil {
			return
		}
		failures++
		delay = retry.base
		for i := 1; i < failures && delay < retry.max; i++ {
			delay *= 2
		}
		delay = min(delay, retry.max)
		if report != nil {
			revision := w.cardHistoryNextRevision(ctx)
			if failures >= retry.stuckAfter {
				err = fmt.Errorf("stuck at revision %s after %d consecutive failures (%s), next retry in %s: %w", revision, failures, maintenanceErrorClass(err), delay, err)
			} else {
				err = fmt.Errorf("revision %s (%s), retry %d in %s: %w", revision, maintenanceErrorClass(err), failures, delay, err)
			}
			report(err)
		}
		timer.Reset(delay)
	}
}

// cardHistoryNextRevision names the revision the cursor will process next.
func (w *Workspace) cardHistoryNextRevision(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var revision sql.NullString
	err := w.db.QueryRowContext(ctx, `SELECT (SELECT revision_id FROM card_revisions WHERE revision_id>j.cursor ORDER BY revision_id LIMIT 1) FROM `+cardHistoryJobTable+` j WHERE singleton=1`).Scan(&revision)
	if err != nil || !revision.Valid {
		return "unknown"
	}
	return revision.String
}
