package storage

import (
	"context"
	"database/sql"
)

// installOutcomeFacts adds the facts a human-level Watch and Decide read:
//
//   - brief_notes.automatic marks reminders core wrote itself, so a card's
//     headline note is the newest thing its owner said, never bookkeeping.
//   - brief_notes.kind/title carry an "fyi": something an agent tells the
//     human that wants no answer. The partial index serves the newest-first
//     page the brief reads.
//   - standing_rulings holds answers a human made durable, keyed by the
//     question's title key, so a covered question never returns to Decide.
//
// The note facts are set by a trigger on brief_notes rather than by widening
// the insert and restore triggers, so every path that writes a note row (new
// event, restore from trash, an older migration's backfill) gets them.
func installOutcomeFacts(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"brief_notes", "events"} {
		has, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !has {
			return err
		}
	}
	for _, column := range []struct{ name, definition string }{
		{"automatic", "INTEGER NOT NULL DEFAULT 0"},
		{"kind", "TEXT NOT NULL DEFAULT ''"},
		{"title", "TEXT NOT NULL DEFAULT ''"},
	} {
		has, err := sqliteTableHasColumn(ctx, tx, "brief_notes", column.name)
		if err != nil {
			return err
		}
		if !has {
			if _, err = tx.ExecContext(ctx, `ALTER TABLE brief_notes ADD COLUMN `+column.name+` `+column.definition); err != nil {
				return err
			}
		}
	}
	facts := func(event string) string {
		return `automatic=COALESCE((SELECT e.actor_id='system' OR json_extract(e.payload_json,'$.payload.automatic')=1 FROM events e WHERE e.id=` + event + `),0),
  kind=COALESCE((SELECT CASE WHEN json_extract(e.payload_json,'$.payload.kind')='fyi' THEN 'fyi' ELSE '' END FROM events e WHERE e.id=` + event + `),''),
  title=COALESCE((SELECT substr(COALESCE(json_extract(e.payload_json,'$.payload.title'),''),1,160) FROM events e WHERE e.id=` + event + `),'')`
	}
	for _, query := range []string{
		`CREATE TRIGGER IF NOT EXISTS brief_notes_outcome_facts AFTER INSERT ON brief_notes BEGIN UPDATE brief_notes SET ` + facts("NEW.event_id") + ` WHERE event_id=NEW.event_id; END`,
		`UPDATE brief_notes SET ` + facts("brief_notes.event_id") + ` WHERE true`,
		`CREATE INDEX IF NOT EXISTS idx_brief_notes_fyi ON brief_notes(ts DESC) WHERE kind='fyi'`,
		`CREATE INDEX IF NOT EXISTS idx_brief_notes_authored ON brief_notes(card_id,ts DESC) WHERE automatic=0`,
		`CREATE TABLE IF NOT EXISTS standing_rulings(
			id TEXT PRIMARY KEY,
			title_key TEXT NOT NULL,
			requester TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL,
			question TEXT NOT NULL,
			answer TEXT NOT NULL DEFAULT '',
			source_ask_id TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL,
			created_at TEXT NOT NULL,
			uses INTEGER NOT NULL DEFAULT 0,
			last_used_at TEXT,
			revoked_at TEXT,
			revoked_by TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_standing_rulings_key ON standing_rulings(title_key,requester) WHERE revoked_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_standing_rulings_recent ON standing_rulings(created_at DESC,id)`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}
