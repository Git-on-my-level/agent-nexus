package storage

import (
	"agent-nexus-core/internal/sqliteutil"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Lifecycle is a rebuildable projection, never an authorization grant. Keep it
// in the canonical mutation's transaction so read selectors can skip inactive
// notifications in an index. Ownership is still checked independently by every
// consuming statement, including after a lifecycle change or revocation.
func indexInboxLifecycle(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "derived_inbox_items")
	if err != nil || !exists {
		return err
	}
	column, err := sqliteTableHasColumn(ctx, tx, "derived_inbox_items", "lifecycle_hidden")
	if err != nil {
		return err
	}
	if !column {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE derived_inbox_items ADD COLUMN lifecycle_hidden INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	ready, err := sqliteTableHasColumn(ctx, tx, "derived_inbox_items", "lifecycle_ready")
	if err != nil {
		return err
	}
	if !ready {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE derived_inbox_items ADD COLUMN lifecycle_ready INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	statements := []string{

		`CREATE TABLE IF NOT EXISTS inbox_lifecycle_refs(inbox_id TEXT NOT NULL,ref TEXT NOT NULL,PRIMARY KEY(inbox_id,ref)) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_inbox_lifecycle_ref ON inbox_lifecycle_refs(ref,inbox_id)`,
		`CREATE TABLE IF NOT EXISTS inbox_hidden_subject_refs(owner_kind TEXT NOT NULL,owner_id TEXT NOT NULL,ref TEXT NOT NULL,PRIMARY KEY(owner_kind,owner_id,ref)) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_inbox_hidden_subject_ref ON inbox_hidden_subject_refs(ref)`,
		`CREATE TABLE IF NOT EXISTS inbox_lifecycle_dirty(inbox_id TEXT PRIMARY KEY) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS inbox_lifecycle_job(singleton INTEGER PRIMARY KEY CHECK(singleton=1),phase INTEGER NOT NULL DEFAULT 0,cursor TEXT NOT NULL DEFAULT '',owners_ready INTEGER NOT NULL DEFAULT 0,done INTEGER NOT NULL DEFAULT 0)`,
		`INSERT OR IGNORE INTO inbox_lifecycle_job(singleton,phase,owners_ready,done) SELECT 1,CASE WHEN populated THEN 0 ELSE 5 END,NOT populated,NOT populated FROM (SELECT (EXISTS(SELECT 1 FROM boards LIMIT 1) OR EXISTS(SELECT 1 FROM documents LIMIT 1) OR EXISTS(SELECT 1 FROM threads LIMIT 1) OR EXISTS(SELECT 1 FROM topics LIMIT 1) OR EXISTS(SELECT 1 FROM cards LIMIT 1) OR EXISTS(SELECT 1 FROM derived_inbox_items LIMIT 1)) AS populated)`,
		`DROP TRIGGER IF EXISTS access_epoch_derived_inbox_items_update`,
		`CREATE TRIGGER access_epoch_derived_inbox_items_update AFTER UPDATE OF id,thread_id,category,trigger_at,due_at,has_due_at,source_event_id,source_card_id,generated_at,data_json,source_hash ON derived_inbox_items BEGIN UPDATE resource_access_epoch SET version=version+1 WHERE singleton=1; END`,
		`DROP VIEW IF EXISTS inbox_lifecycle_subjects`,
		inboxLifecycleSubjectsView(),
	}
	for _, q := range statements {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("inbox lifecycle projection: %w", err)
		}
	}
	for _, event := range []string{"INSERT", "UPDATE OF id,data_json,thread_id,category"} {
		name := "inbox_lifecycle_insert"
		if strings.HasPrefix(event, "UPDATE") {
			name = "inbox_lifecycle_update"
		}
		body := `DELETE FROM inbox_lifecycle_refs WHERE inbox_id=NEW.id;
 INSERT OR IGNORE INTO inbox_lifecycle_refs ` + inboxLifecycleRefsSQL("i.id=NEW.id") + `;
 UPDATE derived_inbox_items SET lifecycle_hidden=` + inboxLifecycleHiddenSQL() + `,lifecycle_ready=(SELECT owners_ready FROM inbox_lifecycle_job WHERE singleton=1) WHERE id=NEW.id;`
		if strings.HasPrefix(event, "UPDATE") {
			body = `DELETE FROM inbox_lifecycle_refs WHERE inbox_id=OLD.id;` + body
		}
		if err = replaceInboxLifecycleTrigger(ctx, tx, name, event, "derived_inbox_items", body); err != nil {
			return err
		}
	}
	if err = replaceInboxLifecycleTrigger(ctx, tx, "inbox_lifecycle_delete", "DELETE", "derived_inbox_items", `DELETE FROM inbox_lifecycle_refs WHERE inbox_id=OLD.id;`); err != nil {
		return err
	}
	for _, source := range []struct{ table, kind string }{{"boards", "board"}, {"documents", "document"}, {"threads", "thread"}, {"topics", "topic"}, {"cards", "card"}, {"work_metadata", ""}} {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			identities := []string{}
			for _, side := range []string{"OLD", "NEW"} {
				if event == "INSERT" && side == "OLD" || event == "DELETE" && side == "NEW" {
					continue
				}
				if source.kind != "" {
					identities = append(identities, "(owner_kind='"+source.kind+"' AND owner_id="+side+".id)")
				}
				switch source.table {
				case "boards":
					identities = append(identities, "(owner_kind='card' AND owner_id IN (SELECT id FROM cards WHERE board_id="+side+".id))")
				case "topics":
					identities = append(identities, "(owner_kind='card' AND owner_id IN (SELECT card_id FROM work_metadata WHERE json_extract(metadata_json,'$.project_ref') IN ('topic:'||"+side+".id,'topic:'||"+side+".handle)))")
				case "work_metadata":
					identities = append(identities, "(owner_kind='card' AND owner_id="+side+".card_id)")
				}
			}
			owners := strings.Join(identities, " OR ")
			body := `INSERT OR IGNORE INTO inbox_lifecycle_dirty SELECT r.inbox_id FROM inbox_lifecycle_refs r JOIN inbox_hidden_subject_refs h ON h.ref=r.ref WHERE ` + owners + `;
 DELETE FROM inbox_hidden_subject_refs WHERE ` + owners + `;
 INSERT OR IGNORE INTO inbox_hidden_subject_refs SELECT owner_kind,owner_id,ref FROM inbox_lifecycle_subjects WHERE ` + owners + `;
 INSERT OR IGNORE INTO inbox_lifecycle_dirty SELECT r.inbox_id FROM inbox_lifecycle_refs r JOIN inbox_hidden_subject_refs h ON h.ref=r.ref WHERE ` + owners + `;
 UPDATE derived_inbox_items SET lifecycle_hidden=` + inboxLifecycleHiddenSQL() + `,lifecycle_ready=(SELECT owners_ready FROM inbox_lifecycle_job WHERE singleton=1) WHERE id IN (SELECT inbox_id FROM inbox_lifecycle_dirty);
 DELETE FROM inbox_lifecycle_dirty;`
			triggerEvent := event
			if event == "UPDATE" {
				columns := "id,handle,thread_id,archived_at,trashed_at"
				if source.table == "cards" {
					columns += ",board_id"
				} else if source.table == "threads" {
					columns = "id,archived_at,trashed_at"
				} else if source.table == "work_metadata" {
					columns = "card_id,metadata_json"
				}
				triggerEvent += " OF " + columns
			}
			if err = replaceInboxLifecycleTrigger(ctx, tx, "inbox_lifecycle_"+source.table+"_"+strings.ToLower(event), triggerEvent, source.table, body); err != nil {
				return err
			}
		}
	}
	// The existing identity projection depends only on inbox ID. Avoid deleting
	// and reinserting that identity (and bumping the ownership epoch) when only
	// lifecycle maintenance columns change. Preserve the canonical trigger body.
	var identityTrigger string
	err = tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='trigger' AND name='mention_identity_derived_inbox_items_update'`).Scan(&identityTrigger)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && strings.Contains(identityTrigger, "AFTER UPDATE ON derived_inbox_items") {
		if _, err = tx.ExecContext(ctx, `DROP TRIGGER mention_identity_derived_inbox_items_update`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, strings.Replace(identityTrigger, "AFTER UPDATE ON derived_inbox_items", "AFTER UPDATE OF id ON derived_inbox_items", 1)); err != nil {
			return err
		}
	}
	return nil
}

func replaceInboxLifecycleTrigger(ctx context.Context, tx *sql.Tx, name, event, table, body string) error {
	if _, err := tx.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+name); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "CREATE TRIGGER "+name+" AFTER "+event+" ON "+table+" BEGIN "+body+" END")
	return err
}

// Match the payload shaper: explicit related_refs supersede legacy refs; when
// absent or empty, legacy refs and the indexed backing thread are used.
func inboxLifecycleRefsSQL(where string) string {
	explicit := `json_type(i.data_json,'$.related_refs')='array' AND json_array_length(i.data_json,'$.related_refs')>0 AND NOT EXISTS (SELECT 1 FROM json_each(i.data_json,'$.related_refs') WHERE type<>'text')`
	legacy := `json_type(i.data_json,'$.refs')='array' AND NOT EXISTS (SELECT 1 FROM json_each(i.data_json,'$.refs') WHERE type<>'text')`
	refs := `SELECT i.id AS inbox_id,CASE WHEN (` + explicit + `) THEN j.value ELSE anx_unicode_trim(j.value) END AS ref FROM derived_inbox_items i,json_each(CASE WHEN (` + explicit + `) THEN json_extract(i.data_json,'$.related_refs') ELSE CASE WHEN (` + legacy + `) THEN json_extract(i.data_json,'$.refs') ELSE '[]' END END) j WHERE j.type='text' AND (` + where + `)
 UNION ALL SELECT i.id,'thread:'||anx_unicode_trim(i.thread_id) FROM derived_inbox_items i WHERE NOT COALESCE((` + explicit + `),0) AND (` + where + `)`
	return `SELECT inbox_id,ref FROM (` + refs + `) WHERE ref<>''`
}

func inboxLifecycleHiddenSQL() string {
	return `anx_unicode_lower(anx_unicode_trim(COALESCE(NULLIF(anx_unicode_trim(CASE WHEN json_type(data_json,'$.kind')='text' THEN json_extract(data_json,'$.kind') ELSE '' END),''),category))) NOT IN ('ask','review','escalate') AND EXISTS (SELECT 1 FROM inbox_lifecycle_refs r JOIN inbox_hidden_subject_refs h ON h.ref=r.ref WHERE r.inbox_id=derived_inbox_items.id)`
}

func inboxLifecycleSubjectsView() string {
	parts := []string{}
	for _, resource := range []struct{ kind, table string }{{"board", "boards"}, {"document", "documents"}, {"thread", "threads"}, {"topic", "topics"}, {"card", "cards"}} {
		thread, handle := "r.thread_id", "r.handle"
		if resource.kind == "thread" {
			thread, handle = "r.id", "''"
		}
		hidden := `COALESCE(r.archived_at,'')<>'' OR COALESCE(r.trashed_at,'')<>''`
		if resource.kind == "card" {
			hidden += ` OR EXISTS (SELECT 1 FROM boards b WHERE b.id=r.board_id AND (COALESCE(b.archived_at,'')<>'' OR COALESCE(b.trashed_at,'')<>'')) OR EXISTS (SELECT 1 FROM work_metadata wm JOIN topics t ON substr(json_extract(wm.metadata_json,'$.project_ref'),1,6)='topic:' AND (t.id=substr(json_extract(wm.metadata_json,'$.project_ref'),7) OR (t.handle=substr(json_extract(wm.metadata_json,'$.project_ref'),7) AND t.handle IS NOT NULL AND trim(t.handle)<>'')) WHERE wm.card_id=r.id AND (COALESCE(t.archived_at,'')<>'' OR COALESCE(t.trashed_at,'')<>''))`
		}
		parts = append(parts, `SELECT '`+resource.kind+`' AS owner_kind,r.id AS owner_id,j.value AS ref FROM `+resource.table+` r,json_each(json_array('`+resource.kind+`:'||r.id,CASE WHEN COALESCE(`+handle+`,'')<>'' THEN '`+resource.kind+`:'||`+handle+` END,CASE WHEN COALESCE(`+thread+`,'')<>'' THEN 'thread:'||`+thread+` END)) j WHERE j.value IS NOT NULL AND (`+hidden+`)`)
	}
	return `CREATE VIEW inbox_lifecycle_subjects AS ` + strings.Join(parts, " UNION ALL ")
}

// MaintainInboxLifecycleBatch advances a durable keyset cursor in a short write
// transaction. Startup installs only empty-table DDL and triggers; readiness is
// independent of this maintenance. Pending rows use canonical lifecycle reads.
func (w *Workspace) MaintainInboxLifecycleBatch(ctx context.Context, limit int) (bool, error) {
	if limit < 1 || limit > 200 {
		limit = 200
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunk(ctx, w.db)
	if err != nil {
		return false, err
	}
	defer cleanup()
	var phase, done int
	var cursor string
	if err = tx.QueryRowContext(ctx, `SELECT phase,cursor,done FROM inbox_lifecycle_job WHERE singleton=1`).Scan(&phase, &cursor, &done); err != nil {
		return false, err
	}
	if done != 0 {
		return true, nil
	}
	sources := []struct{ table, kind string }{{"boards", "board"}, {"documents", "document"}, {"threads", "thread"}, {"topics", "topic"}, {"cards", "card"}, {"derived_inbox_items", "inbox"}}
	source := sources[phase]
	rows, err := tx.QueryContext(ctx, "SELECT id FROM "+source.table+" WHERE id>? ORDER BY id LIMIT ?", cursor, limit)
	if err != nil {
		return false, err
	}
	ids := []any{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
		cursor = id
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(ids) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		if source.kind == "inbox" {
			if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO inbox_lifecycle_refs `+inboxLifecycleRefsSQL("i.id IN ("+marks+")"), append(append([]any{}, ids...), ids...)...); err != nil {
				return false, err
			}
			_, err = tx.ExecContext(ctx, `UPDATE derived_inbox_items SET lifecycle_hidden=`+inboxLifecycleHiddenSQL()+`,lifecycle_ready=1 WHERE id IN (`+marks+`)`, ids...)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO inbox_hidden_subject_refs SELECT owner_kind,owner_id,ref FROM inbox_lifecycle_subjects WHERE owner_kind='`+source.kind+`' AND owner_id IN (`+marks+`)`, ids...)
		}
		if err != nil {
			return false, err
		}
	}
	if len(ids) < limit {
		phase++
		cursor = ""
	}
	done = 0
	if phase == len(sources) {
		done = 1
		phase = len(sources) - 1
	}
	ownersReady := 0
	if phase >= len(sources)-1 {
		ownersReady = 1
	}
	if _, err = tx.ExecContext(ctx, `UPDATE inbox_lifecycle_job SET phase=?,cursor=?,owners_ready=?,done=? WHERE singleton=1`, phase, cursor, ownersReady, done); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return done != 0, nil
}

// RunInboxLifecycleMaintenance is owned by the server's maintenance context.
// Cancellation interrupts the current batch; committed cursors survive restart.
func (w *Workspace) RunInboxLifecycleMaintenance(ctx context.Context, report func(error)) {
	timer := time.NewTicker(50 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			done, err := w.MaintainInboxLifecycleBatch(ctx, 200)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if report != nil {
					report(err)
				}
			} else if done {
				return
			}
		}
	}
}

// Earlier unreleased inbox previews may be marked applied without the readiness
// column. Reconcile only missing metadata, never rebuild their populated state.
func reconcileInboxLifecyclePreview(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	exists, err := sqliteTableExists(ctx, tx, "derived_inbox_items")
	if err != nil || !exists {
		return err
	}
	ready, err := sqliteTableHasColumn(ctx, tx, "derived_inbox_items", "lifecycle_ready")
	if err != nil || ready {
		return err
	}
	if err = indexInboxLifecycle(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
