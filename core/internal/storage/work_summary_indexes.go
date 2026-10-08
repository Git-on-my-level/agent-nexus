package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

// This index stores traversal hints only. Bounded candidates must always be
// joined back through the scoped canonical inbox relation before publication.
func installWorkSummaryAttention(ctx context.Context, tx *sql.Tx) error {
	if exists, err := sqliteTableExists(ctx, tx, "ref_edges"); err != nil {
		return err
	} else if exists {
		if _, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_ref_edges_board_card_page ON ref_edges(source_type,source_id,edge_type,target_type,target_id)`); err != nil {
			return err
		}
	}
	if err := repairPrimaryCardPhases(ctx, tx); err != nil {
		return err
	}
	// Sparse legacy migration fixtures need not contain canonical events.
	events, err := sqliteTableExists(ctx, tx, "events")
	if err != nil {
		return err
	}
	if events {
		if _, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_events_card_summary_message ON events(thread_id,ts DESC,id DESC) WHERE type='message_posted' AND trashed_at IS NULL AND archived_at IS NULL`); err != nil {
			return err
		}
	}
	exists, err := sqliteTableExists(ctx, tx, "derived_inbox_items")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS work_summary_asks(
 subject_ref TEXT NOT NULL,recipient_actor_id TEXT NOT NULL,inbox_id TEXT NOT NULL,trigger_at TEXT NOT NULL,
 PRIMARY KEY(subject_ref,recipient_actor_id,trigger_at,inbox_id)
 ) WITHOUT ROWID;
 CREATE INDEX IF NOT EXISTS idx_work_summary_asks_inbox ON work_summary_asks(inbox_id);
 CREATE TABLE IF NOT EXISTS work_summary_asks_job(singleton INTEGER PRIMARY KEY CHECK(singleton=1),cursor TEXT NOT NULL DEFAULT '',done INTEGER NOT NULL DEFAULT 0);
 INSERT OR IGNORE INTO work_summary_asks_job(singleton) VALUES(1);
 CREATE TRIGGER IF NOT EXISTS work_summary_asks_insert AFTER INSERT ON derived_inbox_items BEGIN
 INSERT OR IGNORE INTO work_summary_asks `+workSummaryAskSelect("NEW.id")+`;
 END;
 CREATE TRIGGER IF NOT EXISTS work_summary_asks_update AFTER UPDATE ON derived_inbox_items BEGIN
 DELETE FROM work_summary_asks WHERE inbox_id=OLD.id;
 INSERT OR IGNORE INTO work_summary_asks `+workSummaryAskSelect("NEW.id")+`;
 END;
 CREATE TRIGGER IF NOT EXISTS work_summary_asks_delete AFTER DELETE ON derived_inbox_items BEGIN
 DELETE FROM work_summary_asks WHERE inbox_id=OLD.id;
 END;`)
	return err
}
func workSummaryAskSelect(id string) string {
	return `SELECT json_extract(data_json,'$.subject_ref'),COALESCE(json_extract(data_json,'$.recipient_actor_id'),''),id,trigger_at
 FROM derived_inbox_items WHERE id=` + id + ` AND COALESCE(json_extract(data_json,'$.kind'),category)='ask' AND json_extract(data_json,'$.subject_ref') LIKE 'card:%'`
}

// Share the existing bounded inbox maintenance lifecycle. Reads never repair or
// scan historical inbox rows; trigger-backed new writes are immediately indexed.
func maintainWorkSummaryAttention(ctx context.Context, tx *sql.Tx) (bool, error) {
	var cursor string
	var done bool
	if err := tx.QueryRowContext(ctx, `SELECT cursor,done FROM work_summary_asks_job WHERE singleton=1`).Scan(&cursor, &done); err != nil {
		return false, err
	}
	if done {
		return true, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM derived_inbox_items WHERE id>? ORDER BY id LIMIT 64`, cursor)
	if err != nil {
		return false, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
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
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO work_summary_asks `+workSummaryAskSelect("?"), id); err != nil {
			return false, err
		}
		cursor = id
	}
	done = len(ids) == 0
	_, err = tx.ExecContext(ctx, `UPDATE work_summary_asks_job SET cursor=?,done=? WHERE singleton=1`, cursor, done)
	return done, err
}

// Board moves historically updated membership metadata without the primary
// card column. Restore the canonical typed phase once, in bounded migration
// pages, then keep it atomic with membership writes. This changes neither
// activity timestamps nor events and never materializes metadata on a read.
func repairPrimaryCardPhases(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"cards", "ref_edges"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !exists {
			return err
		}
	}
	for _, column := range []string{"board_id", "column_key"} {
		exists, err := sqliteTableHasColumn(ctx, tx, "cards", column)
		if err != nil || !exists {
			return err
		}
	}
	const phase = `json_extract(NEW.metadata_json,'$.column_key')`
	const allowed = ` IN ('backlog','in_progress','review','blocked','done','cancelled')`
	for _, operation := range []string{"INSERT", "UPDATE"} {
		_, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS work_summary_primary_phase_`+operation+` AFTER `+operation+` ON ref_edges
 WHEN NEW.source_type='board' AND NEW.target_type='card' AND NEW.edge_type='board_card' AND `+phase+allowed+`
 BEGIN UPDATE cards SET column_key=`+phase+` WHERE id=NEW.target_id AND board_id=NEW.source_id AND column_key<>`+phase+`; END`)
		if err != nil {
			return err
		}
	}
	cursor := ""
	for {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM cards WHERE id>? ORDER BY id LIMIT 64`, cursor)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		// The unique membership index supplies one primary edge per card.
		const primaryPhase = `(SELECT json_extract(re.metadata_json,'$.column_key') FROM ref_edges re WHERE re.source_type='board' AND re.source_id=cards.board_id AND re.target_type='card' AND re.target_id=cards.id AND re.edge_type='board_card')`
		raw, _ := json.Marshal(ids)
		if _, err = tx.ExecContext(ctx, `UPDATE cards SET column_key=`+primaryPhase+` WHERE id IN (SELECT value FROM json_each(?)) AND `+primaryPhase+allowed+` AND column_key<>`+primaryPhase, string(raw)); err != nil {
			return err
		}
		cursor = ids[len(ids)-1]
	}
}
