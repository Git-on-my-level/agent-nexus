package storage

import (
	"agent-nexus-core/internal/workprojection"
	"context"
	"database/sql"
	"time"
)

// New empty indexes are populated by a resumable maintenance cursor, never by
// scanning historical event payloads on startup or on request reads.
// askEffectiveClosedSQL shares source-fact precedence with work/report projection.
func askEffectiveClosedSQL() string { return workprojection.ClosedSQL() }

const askWorkJoins = ` LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id `

func installAskSubjects(ctx context.Context, tx *sql.Tx) error {

	for _, table := range []string{"events", "cards", "human_attention_request_resolutions"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS ask_subjects(ask_id TEXT PRIMARY KEY,card_id TEXT NOT NULL,requester TEXT NOT NULL,thread_id TEXT NOT NULL,open INTEGER NOT NULL DEFAULT 1,due_at REAL NOT NULL DEFAULT 1e20,close_reason TEXT NOT NULL DEFAULT 'expired')`,
		`CREATE INDEX IF NOT EXISTS ask_subjects_card_open ON ask_subjects(card_id,open,ask_id)`,
		`CREATE INDEX IF NOT EXISTS ask_subjects_due ON ask_subjects(open,due_at,ask_id)`,
		`CREATE TABLE IF NOT EXISTS ask_subjects_job(singleton INTEGER PRIMARY KEY CHECK(singleton=1),cursor TEXT NOT NULL DEFAULT '',done INTEGER NOT NULL DEFAULT 0)`,
		`INSERT OR IGNORE INTO ask_subjects_job(singleton) VALUES(1)`,
		`CREATE TABLE IF NOT EXISTS ask_subject_close_queue(card_id TEXT PRIMARY KEY)`,
		`CREATE TRIGGER IF NOT EXISTS ask_subjects_insert AFTER INSERT ON events WHEN NEW.type='human_attention_requested' BEGIN
 INSERT OR IGNORE INTO ask_subjects(ask_id,card_id,requester,thread_id,open,due_at,close_reason)
 SELECT NEW.id,COALESCE(c.id,''),COALESCE(json_extract(NEW.payload_json,'$.payload.requester_actor_id'),NEW.actor_id),COALESCE(NEW.thread_id,''),1,
 CASE WHEN ` + askEffectiveClosedSQL() + ` THEN 0 ELSE COALESCE(julianday(json_extract(NEW.payload_json,'$.payload.expires_at')),1e20) END,
 CASE WHEN ` + askEffectiveClosedSQL() + ` THEN 'subject_closed' ELSE 'expired' END
 FROM (SELECT 1) seed LEFT JOIN cards c ON substr(json_extract(NEW.payload_json,'$.payload.subject_ref'),1,5)='card:' AND c.id=COALESCE((SELECT id FROM cards WHERE handle=anx_normalize_handle(substr(json_extract(NEW.payload_json,'$.payload.subject_ref'),6))),(SELECT resource_id FROM resource_handle_aliases WHERE resource_type='card' AND alias_handle=anx_normalize_handle(substr(json_extract(NEW.payload_json,'$.payload.subject_ref'),6))),substr(json_extract(NEW.payload_json,'$.payload.subject_ref'),6))` + askWorkJoins + `; END`,
		`CREATE TRIGGER IF NOT EXISTS ask_subjects_resolve AFTER INSERT ON human_attention_request_resolutions BEGIN UPDATE ask_subjects SET open=0 WHERE ask_id=NEW.request_event_id; END`,
		`CREATE TRIGGER IF NOT EXISTS ask_subjects_close AFTER UPDATE OF column_key,archived_at,trashed_at ON cards BEGIN INSERT INTO ask_subject_close_queue(card_id) SELECT c.id FROM cards c ` + askWorkJoins + ` WHERE c.id=NEW.id AND ` + askEffectiveClosedSQL() + ` ON CONFLICT DO NOTHING; END`,
		`CREATE TRIGGER IF NOT EXISTS ask_subjects_metadata_insert AFTER INSERT ON work_metadata BEGIN INSERT INTO ask_subject_close_queue(card_id) SELECT c.id FROM cards c ` + askWorkJoins + ` WHERE c.id=NEW.card_id AND ` + askEffectiveClosedSQL() + ` ON CONFLICT DO NOTHING; END`,
		`CREATE TRIGGER IF NOT EXISTS ask_subjects_metadata_update AFTER UPDATE OF metadata_json,authority,latest_observation_id ON work_metadata BEGIN INSERT INTO ask_subject_close_queue(card_id) SELECT c.id FROM cards c ` + askWorkJoins + ` WHERE c.id=NEW.card_id AND ` + askEffectiveClosedSQL() + ` ON CONFLICT DO NOTHING; END`,
	}

	for _, q := range statements {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func (w *Workspace) MaintainAskSubjectsBatch(ctx context.Context) (bool, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var cursor string
	var done bool
	if err = tx.QueryRowContext(ctx, `SELECT cursor,done FROM ask_subjects_job WHERE singleton=1`).Scan(&cursor, &done); err != nil || done {
		return done, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM events WHERE id>? ORDER BY id LIMIT 200`, cursor)
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
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO ask_subjects(ask_id,card_id,requester,thread_id,open,due_at,close_reason) SELECT e.id,COALESCE(c.id,''),COALESCE(json_extract(e.payload_json,'$.payload.requester_actor_id'),e.actor_id),COALESCE(e.thread_id,''),NOT EXISTS(SELECT 1 FROM human_attention_request_resolutions r WHERE r.request_event_id=e.id),CASE WHEN `+askEffectiveClosedSQL()+` THEN 0 ELSE COALESCE(julianday(json_extract(e.payload_json,'$.payload.expires_at')),1e20) END,CASE WHEN `+askEffectiveClosedSQL()+` THEN 'subject_closed' ELSE 'expired' END FROM events e LEFT JOIN cards c ON substr(json_extract(e.payload_json,'$.payload.subject_ref'),1,5)='card:' AND c.id=COALESCE((SELECT id FROM cards WHERE handle=anx_normalize_handle(substr(json_extract(e.payload_json,'$.payload.subject_ref'),6))),(SELECT resource_id FROM resource_handle_aliases WHERE resource_type='card' AND alias_handle=anx_normalize_handle(substr(json_extract(e.payload_json,'$.payload.subject_ref'),6))),substr(json_extract(e.payload_json,'$.payload.subject_ref'),6))`+askWorkJoins+` WHERE e.id=? AND e.type='human_attention_requested'`, id)
		if err != nil {
			return false, err
		}
		// Existing indexed asks also need closure reconciliation after upgrade.
		if _, err = tx.ExecContext(ctx, `INSERT INTO ask_subject_close_queue(card_id) SELECT c.id FROM ask_subjects a JOIN cards c ON c.id=a.card_id `+askWorkJoins+` WHERE a.ask_id=? AND a.open=1 AND `+askEffectiveClosedSQL()+` ON CONFLICT DO NOTHING`, id); err != nil {
			return false, err
		}
		cursor = id
	}
	done = len(ids) < 200
	if _, err = tx.ExecContext(ctx, `UPDATE ask_subjects_job SET cursor=?,done=? WHERE singleton=1`, cursor, done); err != nil {
		return false, err
	}
	return done, tx.Commit()
}
func (w *Workspace) RunAskSubjectsMaintenance(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			done, err := w.MaintainAskSubjectsBatch(ctx)
			if err != nil {
				if report != nil {
					report(err)
				}
			} else if done {
				return
			}
		}
	}
}

// Upgrade earlier ask previews without scanning populated business tables.
func installAskEffectiveClosure(ctx context.Context, tx *sql.Tx) error {
	for _, name := range []string{"ask_subjects_insert", "ask_subjects_close"} {
		if _, err := tx.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+name); err != nil {
			return err
		}
	}
	if err := installAskSubjects(ctx, tx); err != nil {
		return err
	}
	exists, err := sqliteTableExists(ctx, tx, "ask_subjects_job")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE ask_subjects_job SET cursor='',done=0 WHERE singleton=1`)
	return err
}
