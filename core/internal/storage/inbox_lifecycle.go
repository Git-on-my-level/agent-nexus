package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_work_metadata_project_ref ON work_metadata(json_extract(metadata_json,'$.project_ref'),card_id)`,
		`CREATE TABLE IF NOT EXISTS inbox_lifecycle_refs(inbox_id TEXT NOT NULL,ref TEXT NOT NULL,PRIMARY KEY(inbox_id,ref)) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_inbox_lifecycle_ref ON inbox_lifecycle_refs(ref,inbox_id)`,
		`CREATE TABLE IF NOT EXISTS inbox_hidden_subject_refs(owner_kind TEXT NOT NULL,owner_id TEXT NOT NULL,ref TEXT NOT NULL,PRIMARY KEY(owner_kind,owner_id,ref)) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_inbox_hidden_subject_ref ON inbox_hidden_subject_refs(ref)`,
		`CREATE TABLE IF NOT EXISTS inbox_lifecycle_dirty(inbox_id TEXT PRIMARY KEY) WITHOUT ROWID`,
		`DELETE FROM inbox_lifecycle_dirty`,
		`DROP VIEW IF EXISTS inbox_lifecycle_subjects`,
		inboxLifecycleSubjectsView(),
		`DELETE FROM inbox_lifecycle_refs`,
		`INSERT OR IGNORE INTO inbox_lifecycle_refs ` + inboxLifecycleRefsSQL("1=1"),
		`DELETE FROM inbox_hidden_subject_refs`,
		`INSERT OR IGNORE INTO inbox_hidden_subject_refs SELECT owner_kind,owner_id,ref FROM inbox_lifecycle_subjects`,
		`UPDATE derived_inbox_items SET lifecycle_hidden=` + inboxLifecycleHiddenSQL(),
		`CREATE INDEX IF NOT EXISTS idx_inbox_all_category_page ON derived_inbox_items(CASE anx_unicode_trim(category) WHEN 'escalate' THEN 0 WHEN 'ask' THEN 1 WHEN 'review' THEN 2 ELSE 99 END,trigger_at DESC,id ASC)`,
		`DROP INDEX IF EXISTS idx_inbox_category_page`,
		`CREATE INDEX idx_inbox_category_page ON derived_inbox_items(CASE anx_unicode_trim(category) WHEN 'escalate' THEN 0 WHEN 'ask' THEN 1 WHEN 'review' THEN 2 ELSE 99 END,trigger_at DESC,id ASC) WHERE lifecycle_hidden=0`,
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
 UPDATE derived_inbox_items SET lifecycle_hidden=` + inboxLifecycleHiddenSQL() + ` WHERE id=NEW.id;`
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
 UPDATE derived_inbox_items SET lifecycle_hidden=` + inboxLifecycleHiddenSQL() + ` WHERE id IN (SELECT inbox_id FROM inbox_lifecycle_dirty);
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
