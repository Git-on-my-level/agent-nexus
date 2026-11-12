// Card revision manifests keep their complete reference set in the raw access
// edges, but the stored artifact manifest does not need the prose bodies that
// produced those atoms. Writers insert the complete manifest first (so the
// insert trigger creates every raw edge), then prune it to the reference-only
// form once the revision row exists.
//
// Migration 142 installs the artifact-update trigger that recognizes exactly
// that prune and leaves the raw edges alone. A bounded maintenance cursor
// prunes already-indexed legacy artifacts by artifact id. Every other update
// still rebuilds normally, so a genuine manifest change cannot leave stale
// edges. Nothing here can change a decision: only the stored manifest bytes
// shrink.

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"agent-nexus-core/internal/cardcontent"
	"agent-nexus-core/internal/loopstats"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/sqliteutil"
)

const (
	cardManifestJobTable    = "resource_access_card_manifest_job"
	cardManifestBatch       = 16
	cardManifestChunkBudget = 50 * time.Millisecond
	cardManifestRetryBase   = 50 * time.Millisecond
	cardManifestRetryMax    = 5 * time.Minute
)

// installCardManifestPruning installs the prune-recognizing artifact update
// trigger and the durable backfill cursor. It scans no canonical rows; the
// maintenance loop below prunes existing artifacts in bounded transactions.
func installCardManifestPruning(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"artifacts", "card_revisions", "document_revisions", "resource_access_edges"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
	}
	source, ok := artifactOwnershipSource()
	if !ok {
		return fmt.Errorf("artifact ownership source missing")
	}
	columns, err := existingSourceColumns(ctx, tx, source)
	if err != nil {
		return err
	}
	insert := accessSourceInsert(source, columns)
	clear := `DELETE FROM resource_access_edges WHERE source_kind='artifact' AND source_id=OLD.id;`
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS ` + cardManifestJobTable + `(singleton INTEGER PRIMARY KEY CHECK(singleton=1),cursor TEXT NOT NULL DEFAULT '',done INTEGER NOT NULL DEFAULT 0 CHECK(done IN (0,1)))`,
		`INSERT INTO ` + cardManifestJobTable + `(singleton) VALUES(1) ON CONFLICT DO NOTHING`,
		`UPDATE ` + cardManifestJobTable + ` SET cursor='',done=0 WHERE singleton=1`,
		`DROP TRIGGER IF EXISTS access_artifacts_update`,
		artifactManifestUpdateTriggerSQL(clear, insert("NEW.", ""), true, true),
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("install card manifest pruning: %w", err)
		}
	}
	return nil
}

func artifactOwnershipSource() (resourceaccess.OwnershipSource, bool) {
	for _, source := range resourceaccess.OwnershipSources {
		if source.Table == "artifacts" {
			return source, true
		}
	}
	return resourceaccess.OwnershipSource{}, false
}

func existingSourceColumns(ctx context.Context, tx *sql.Tx, source resourceaccess.OwnershipSource) ([]string, error) {
	var columns []string
	for _, column := range source.Columns {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, source.Table, column).Scan(&n); err != nil {
			return nil, err
		}
		if n != 0 {
			columns = append(columns, column)
		}
	}
	return columns, nil
}

// artifactManifestUpdateTriggerSQL rebuilds an artifact's edges on every
// update except the exact prune of a card revision artifact whose content hash
// still matches. A shared document-revision artifact is excluded so its own
// document-history set is never rebuilt from a pruned manifest.
func artifactManifestUpdateTriggerSQL(clear, insert string, hasCardRevisions, hasDocumentRevisions bool) string {
	prune := "0"
	if hasCardRevisions {
		prune = `NEW.id IS OLD.id AND NEW.refs_json IS OLD.refs_json AND NEW.metadata_json IS OLD.metadata_json AND NEW.trash_reason IS OLD.trash_reason AND NEW.content_hash IS OLD.content_hash AND OLD.content_refs_json IS NOT NULL AND NEW.content_refs_json IS NOT NULL AND NEW.content_refs_json = anx_resource_without_prose(CAST(OLD.content_refs_json AS BLOB)) AND EXISTS(SELECT 1 FROM card_revisions cr WHERE cr.artifact_id=OLD.id AND cr.content_hash=NEW.content_hash)`
		if hasDocumentRevisions {
			prune += ` AND NOT EXISTS(SELECT 1 FROM document_revisions dr WHERE dr.artifact_id=OLD.id)`
		}
	}
	return `CREATE TRIGGER IF NOT EXISTS access_artifacts_update AFTER UPDATE OF id,refs_json,metadata_json,content_refs_json,trash_reason ON artifacts WHEN NOT (` + prune + `) BEGIN ` + clear + insert + ` END`
}

// MaintainCardManifestBatch prunes one bounded batch of card revision
// artifacts. The cursor advances even for rows left unpruned, so an artifact
// whose manifest another rule changed (or whose content cannot be verified)
// keeps its bytes without blocking the job.
func (w *Workspace) MaintainCardManifestBatch(ctx context.Context) (bool, error) {
	ctx, finish := loopstats.Start(ctx, "card_manifest")
	defer finish()
	return w.maintainCardManifestChunk(ctx, cardManifestBatch, cardManifestChunkBudget)
}

func (w *Workspace) maintainCardManifestChunk(ctx context.Context, limit int, budget time.Duration) (bool, error) {
	sqlCtx := context.WithoutCancel(ctx)
	var exists bool
	if err := w.db.QueryRowContext(sqlCtx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name=?)`, cardManifestJobTable).Scan(&exists); err != nil || !exists {
		return true, err
	}
	var cursor string
	var done bool
	if err := w.db.QueryRowContext(sqlCtx, `SELECT cursor,done FROM `+cardManifestJobTable+` WHERE singleton=1`).Scan(&cursor, &done); err != nil || done {
		return done, err
	}
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunkWithBusyWait(sqlCtx, w.db, 20*time.Millisecond)
	if err != nil {
		return false, err
	}
	defer cleanup()
	deadline := time.Now().Add(budget)
	done, err = pruneCardManifestsUntil(sqlCtx, tx, cursor, limit, func() bool {
		return ctx.Err() != nil || time.Now().After(deadline)
	})
	if err != nil {
		return false, err
	}
	return done, tx.Commit()
}

// pruneCardManifestsUntil updates whole revision contents from the canonical
// keyframe/delta rows, never from the manifest being pruned. A manifest is
// only rewritten when it still equals the complete manifest derived from
// those bytes; otherwise another writer owns it and it is left untouched.
func pruneCardManifestsUntil(ctx context.Context, tx *sql.Tx, cursor string, limit int, stop func() bool) (bool, error) {
	limit = max(1, limit)
	rows, err := tx.QueryContext(ctx, `SELECT cr.artifact_id, cr.revision_id, cr.content_hash, a.content_hash, a.content_refs_json
		  FROM card_revisions cr JOIN artifacts a ON a.id=cr.artifact_id
		 WHERE cr.artifact_id>? AND a.content_refs_json IS NOT NULL
		   AND NOT EXISTS(SELECT 1 FROM document_revisions dr WHERE dr.artifact_id=cr.artifact_id)
		 ORDER BY cr.artifact_id
		 LIMIT ?`, cursor, limit)
	if err != nil {
		return false, err
	}
	type manifestWork struct {
		artifactID, revisionID string
		revisionHash           string
		artifactHash           sql.NullString
		manifest               string
	}
	var work []manifestWork
	revisionIDs := make([]string, 0, limit)
	for rows.Next() {
		var w manifestWork
		var artifactHash, manifest sql.NullString
		if err := rows.Scan(&w.artifactID, &w.revisionID, &w.revisionHash, &artifactHash, &manifest); err != nil {
			rows.Close()
			return false, err
		}
		w.artifactHash, w.manifest = artifactHash, manifest.String
		work = append(work, w)
		revisionIDs = append(revisionIDs, w.revisionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()

	done := len(work) < limit
	if len(work) == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE `+cardManifestJobTable+` SET done=1 WHERE singleton=1`); err != nil {
			return false, err
		}
		return true, nil
	}
	contents, err := cardcontent.LoadRevisionContents(ctx, tx, revisionIDs)
	if err != nil {
		return false, fmt.Errorf("load card revision content: %w", err)
	}
	pruned := 0
	for _, w := range work {
		if stop() && pruned > 0 {
			done = false
			break
		}
		cursor = w.artifactID
		if !w.artifactHash.Valid || w.artifactHash.String == "" || w.artifactHash.String != w.revisionHash {
			continue
		}
		content, ok := contents[w.revisionID]
		if !ok || cardcontent.Hash(content) != w.revisionHash {
			continue
		}
		complete := string(resourceaccess.ContentReferenceAtomsJSON(string(content), "structured"))
		if w.manifest != complete {
			// Another writer or rule changed this artifact's access inputs.
			continue
		}
		filtered := resourceaccess.WithoutProse(w.manifest)
		if filtered == w.manifest {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE artifacts SET content_refs_json=? WHERE id=? AND content_hash=? AND content_refs_json=?`, filtered, w.artifactID, w.artifactHash.String, w.manifest); err != nil {
			return false, err
		}
		pruned++
	}
	loopstats.Rows(ctx, pruned)
	if _, err := tx.ExecContext(ctx, `UPDATE `+cardManifestJobTable+` SET cursor=?,done=? WHERE singleton=1`, cursor, boolInt(done)); err != nil {
		return false, err
	}
	return done, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// RunCardManifestMaintenance drains the legacy card artifact manifests after
// startup. Failures back off exponentially, capped at five minutes. Leaving a
// manifest unpruned is always safe, so a persistent failure blocks only this
// storage optimization.
func (w *Workspace) RunCardManifestMaintenance(ctx context.Context, report func(error)) {
	delay := cardManifestRetryBase
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		done, err := w.MaintainCardManifestBatch(ctx)
		if err == nil {
			if done {
				return
			}
			delay = cardManifestRetryBase
			timer.Reset(delay)
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if report != nil {
			report(fmt.Errorf("card manifest maintenance: %s: %w", maintenanceErrorClass(err), err))
		}
		delay = min(delay*2, cardManifestRetryMax)
		timer.Reset(delay)
	}
}
