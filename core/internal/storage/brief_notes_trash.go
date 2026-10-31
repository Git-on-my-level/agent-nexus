package storage

import (
	"context"
	"database/sql"
)

// briefNoteExcerptSQL reads a note's text for Watch. Notes store text; body is
// the older field some imported messages still carry; core's own reminders have
// only a summary.
func briefNoteExcerptSQL(payload string) string {
	return `substr(COALESCE(NULLIF(json_extract(` + payload + `,'$.payload.text'),''),NULLIF(json_extract(` + payload + `,'$.payload.body'),''),json_extract(` + payload + `,'$.summary'),''),1,280)`
}

func briefNotesRestoreTrigger() string {
	noteSelect := `SELECT NEW.id,c.id,COALESCE(NEW.actor_id,''),NEW.ts,
  anx_resolve_when(json_extract(NEW.payload_json,'$.payload.next.at'),NEW.ts),
  substr(COALESCE(json_extract(NEW.payload_json,'$.payload.next.what'),''),1,160),
  ` + briefEvidenceSQL + `,
  ` + briefNoteExcerptSQL("NEW.payload_json") + `
 FROM cards c WHERE c.thread_id=NEW.thread_id`
	return `CREATE TRIGGER IF NOT EXISTS brief_notes_restore AFTER UPDATE OF trashed_at ON events
 WHEN NEW.type='message_posted' AND COALESCE(OLD.trashed_at,'')<>'' AND COALESCE(NEW.trashed_at,'')='' AND COALESCE(NEW.thread_id,'')<>''
 BEGIN
 INSERT INTO brief_notes(event_id,card_id,author,ts,next_at,next_what,evidence,excerpt)
 ` + noteSelect + ` ON CONFLICT DO NOTHING;
 INSERT INTO brief_dirty(card_id) SELECT c.id FROM cards c WHERE c.thread_id=NEW.thread_id ON CONFLICT DO NOTHING;
 END`
}

// installBriefNotesTrash drops a note from the promise when its event is
// trashed and puts it back when the event is restored. brief_notes is
// otherwise insert-only, so a trashed message would keep driving the fold.
// Migration 95 also removes notes whose events were already trashed.
func installBriefNotesTrash(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "brief_notes")
	if err != nil || !exists {
		return err
	}
	for _, q := range []string{
		`CREATE TRIGGER IF NOT EXISTS brief_notes_trash AFTER UPDATE OF trashed_at ON events
 WHEN NEW.type='message_posted' AND COALESCE(OLD.trashed_at,'')='' AND COALESCE(NEW.trashed_at,'')<>''
 BEGIN
 INSERT INTO brief_dirty(card_id) SELECT card_id FROM brief_notes WHERE event_id=NEW.id ON CONFLICT DO NOTHING;
 DELETE FROM brief_notes WHERE event_id=NEW.id;
 END`,
		briefNotesRestoreTrigger(),
		`INSERT INTO brief_dirty(card_id) SELECT DISTINCT n.card_id FROM brief_notes n JOIN events e ON e.id=n.event_id WHERE COALESCE(e.trashed_at,'')<>'' ON CONFLICT DO NOTHING`,
		`DELETE FROM brief_notes WHERE event_id IN (SELECT id FROM events WHERE COALESCE(trashed_at,'')<>'')`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// repairBriefNoteExcerpts points the triggers at the field notes actually use
// and recomputes every excerpt written while they read the wrong one.
func repairBriefNoteExcerpts(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "brief_notes")
	if err != nil || !exists {
		return err
	}
	for _, q := range []string{
		`DROP TRIGGER IF EXISTS brief_notes_insert`,
		`DROP TRIGGER IF EXISTS brief_notes_restore`,
		briefNotesInsertTrigger(),
		briefNotesRestoreTrigger(),
		`UPDATE brief_notes SET excerpt=(SELECT ` + briefNoteExcerptSQL("e.payload_json") + ` FROM events e WHERE e.id=brief_notes.event_id) WHERE EXISTS(SELECT 1 FROM events e WHERE e.id=brief_notes.event_id)`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
